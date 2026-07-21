package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"

	"fredb-website"
)

// note this cant be greater than 4gb for now
const MaxValueBytes = 50 * 1024 * 1024 // 50MB

const MaxStorageBytes = 1024 * 1024 * 1024 // 1gb per tenant

const (
	RateLimitCapacityBytes     = 10 * 1024 * 1024 // 10MB
	RateLimitRefillBytesPerSec = 5 * 1024 * 1024  // 5MB/s
	RateLimitFlatOverheadBytes = 1024             // 1KB
	RateLimitWriteByteWeight   = 1.0              // 1.0x
	RateLimitReadByteWeight    = 1.0 / 8.0        // 0.125x
)

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func WithCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Api-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func withLogging(h http.Handler, verbose bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusWriter{ResponseWriter: w, code: 200}
		h.ServeHTTP(rw, r)
		if verbose {
			log.Printf("%s %s -> %d", r.Method, r.URL, rw.code)
		}
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrUint64(key string, fallback uint64) uint64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envOrFloat64(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func main() {
	verbose := flag.Bool("verbose", os.Getenv("FREDB_VERBOSE") == "1", "log requests")
	dataRoot := flag.String("data-root", envOr("FREDB_DATA_ROOT", "/tmp/fredb-data"), "root dir for per-tenant data")
	sockRoot := flag.String("sock-root", envOr("FREDB_SOCK_ROOT", "/tmp/fredb-socks"), "root dir for per-tenant engine sockets")
	apiAddr := flag.String("api-addr", envOr("FREDB_API_ADDR", ":8080"), "api port address")
	websiteAddr := flag.String("website-addr", envOr("FREDB_WEBSITE_ADDR", ":8081"), "website port address")
	maxStorageBytes := flag.Uint64("max-storage-bytes", envOrUint64("FREDB_MAX_STORAGE_BYTES", MaxStorageBytes), "per-tenant storage cap in bytes")
	rateLimitCapacityBytes := flag.Float64("rate-limit-capacity-bytes", envOrFloat64("FREDB_RATE_LIMIT_CAPACITY_BYTES", RateLimitCapacityBytes), "per-tenant rate limit burst capacity, bytes-equivalent")
	rateLimitRefillBytesPerSec := flag.Float64("rate-limit-refill-bytes-per-sec", envOrFloat64("FREDB_RATE_LIMIT_REFILL_BYTES_PER_SEC", RateLimitRefillBytesPerSec), "per-tenant sustained rate limit, bytes-equivalent/sec")
	rateLimitFlatOverheadBytes := flag.Float64("rate-limit-flat-overhead-bytes", envOrFloat64("FREDB_RATE_LIMIT_FLAT_OVERHEAD_BYTES", RateLimitFlatOverheadBytes), "fixed bytes-equivalent cost charged per request regardless of payload size")
	rateLimitWriteByteWeight := flag.Float64("rate-limit-write-byte-weight", envOrFloat64("FREDB_RATE_LIMIT_WRITE_BYTE_WEIGHT", RateLimitWriteByteWeight), "Put byte cost weight")
	rateLimitReadByteWeight := flag.Float64("rate-limit-read-byte-weight", envOrFloat64("FREDB_RATE_LIMIT_READ_BYTE_WEIGHT", RateLimitReadByteWeight), "Get/Range byte cost weight, relative to write weight")
	flag.Parse()

	rateLimit := RateLimitConfig{
		CapacityBytes:     *rateLimitCapacityBytes,
		RefillBytesPerSec: *rateLimitRefillBytesPerSec,
		FlatOverheadBytes: *rateLimitFlatOverheadBytes,
		WriteByteWeight:   *rateLimitWriteByteWeight,
		ReadByteWeight:    *rateLimitReadByteWeight,
	}
	manager := NewDatabaseManager(*dataRoot, *sockRoot, MaxValueBytes, *maxStorageBytes, rateLimit)
	if err := manager.LoadAll(); err != nil {
		log.Fatal(err)
	}

	web := website.NewHandler(manager)
	api := NewAPIHandler(manager)

	go func() {
		log.Printf("website at http://localhost%s", *websiteAddr)
		log.Fatal(http.ListenAndServe(*websiteAddr, withLogging(web, *verbose)))
	}()

	log.Printf("api at http://localhost%s", *apiAddr)
	log.Fatal(http.ListenAndServe(*apiAddr, withLogging(api, *verbose)))
}
