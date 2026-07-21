// hammer sends heavy concurrent traffic at the fredb api to exercise rate
// limiting and let /loadtest show something interesting. see
// HAMMER_SCRIPT_GUIDE.md for the target behavior.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	op      string
	status  int
	latency time.Duration
}

func main() {
	base := flag.String("base", "http://localhost:8080", "api base url")
	apiKey := flag.String("key", "", "already-provisioned api key (required)")
	workers := flag.Int("workers", 30, "concurrent worker count")
	duration := flag.Duration("duration", 30*time.Second, "how long to hammer for")
	flag.Parse()

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "missing -key")
		os.Exit(1)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        *workers * 2,
			MaxIdleConnsPerHost: *workers * 2,
		},
	}

	deadline := time.Now().Add(*duration)
	resultsCh := make(chan []result, *workers)
	var totalSent int64

	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			resultsCh <- runWorker(client, *base, *apiKey, worker, deadline, &totalSent)
		}(w)
	}
	wg.Wait()
	close(resultsCh)

	var all []result
	for rs := range resultsCh {
		all = append(all, rs...)
	}

	summarize(all, *duration)
}

func runWorker(client *http.Client, base, apiKey string, worker int, deadline time.Time, totalSent *int64) []result {
	var out []result
	var putKeys []string
	i := 0

	for time.Now().Before(deadline) {
		i++
		roll := rand.Float64()
		var r result
		switch {
		case roll < 0.70 || len(putKeys) == 0:
			key := fmt.Sprintf("bench:%d:%d", worker, i)
			r = doPut(client, base, apiKey, key)
			putKeys = append(putKeys, key)
			if len(putKeys) > 200 {
				putKeys = putKeys[1:]
			}
		case roll < 0.95:
			key := putKeys[rand.Intn(len(putKeys))]
			r = doGet(client, base, apiKey, key)
		default:
			r = doRange(client, base, apiKey, worker)
		}
		out = append(out, r)
		atomic.AddInt64(totalSent, 1)
	}
	return out
}

func randomValue() []byte {
	n := 200 + rand.Intn(3800) // 200B - 4KB
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func doPut(client *http.Client, base, apiKey, key string) result {
	start := time.Now()
	req, _ := http.NewRequest(http.MethodPut, base+"/key/"+key, bytes.NewReader(randomValue()))
	req.Header.Set("X-Api-Key", apiKey)
	status := do(client, req)
	return result{op: "PUT", status: status, latency: time.Since(start)}
}

func doGet(client *http.Client, base, apiKey, key string) result {
	start := time.Now()
	req, _ := http.NewRequest(http.MethodGet, base+"/key/"+key, nil)
	req.Header.Set("X-Api-Key", apiKey)
	status := do(client, req)
	return result{op: "GET", status: status, latency: time.Since(start)}
}

func doRange(client *http.Client, base, apiKey string, worker int) result {
	start := time.Now()
	prefix := "bench:" + strconv.Itoa(worker) + ":"
	url := base + "/range?start=" + prefix + "&end=" + prefix + "~"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Api-Key", apiKey)
	status := do(client, req)
	return result{op: "RANGE", status: status, latency: time.Since(start)}
}

func do(client *http.Client, req *http.Request) int {
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func summarize(all []result, duration time.Duration) {
	if len(all) == 0 {
		fmt.Println("no requests sent")
		return
	}

	byOp := map[string]int{}
	var limited, errored int
	latencies := make([]float64, 0, len(all))
	for _, r := range all {
		byOp[r.op]++
		switch {
		case r.status == http.StatusTooManyRequests:
			limited++
		case r.status == 0 || r.status >= 400:
			errored++
		}
		latencies = append(latencies, float64(r.latency.Microseconds())/1000.0)
	}
	sort.Float64s(latencies)

	pct := func(p float64) float64 {
		idx := int(p * float64(len(latencies)-1))
		return latencies[idx]
	}

	fmt.Println("=== hammer summary ===")
	fmt.Printf("duration:      %s\n", duration)
	fmt.Printf("total sent:    %d (%.1f req/s)\n", len(all), float64(len(all))/duration.Seconds())
	fmt.Printf("by op:         PUT=%d GET=%d RANGE=%d\n", byOp["PUT"], byOp["GET"], byOp["RANGE"])
	fmt.Printf("rate limited:  %d (%.2f%%)\n", limited, 100*float64(limited)/float64(len(all)))
	fmt.Printf("other errors:  %d (%.2f%%)\n", errored, 100*float64(errored)/float64(len(all)))
	fmt.Printf("latency ms:    min=%.2f p50=%.2f p99=%.2f max=%.2f\n",
		latencies[0], pct(0.50), pct(0.99), latencies[len(latencies)-1])
	fmt.Println("(server-side view: check /stats or /loadtest for handler-time latency, which excludes this client's network RTT)")
}
