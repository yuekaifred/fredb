package main

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"
)

const statsWindowSeconds = 30

type statsBucket struct {
	count     int
	errors    int
	limited   int
	latencies []float64 // milliseconds
}

// StatsTracker keeps a rolling per-second window of request counts, error
// rates, and latency samples for the api server. Global across all tenants.
type StatsTracker struct {
	mu      sync.Mutex
	buckets map[int64]*statsBucket
	total   uint64
}

func NewStatsTracker() *StatsTracker {
	return &StatsTracker{buckets: make(map[int64]*statsBucket)}
}

func (s *StatsTracker) Record(status int, dur time.Duration) {
	sec := time.Now().Unix()

	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.buckets[sec]
	if b == nil {
		b = &statsBucket{}
		s.buckets[sec] = b
		for k := range s.buckets {
			if sec-k > statsWindowSeconds {
				delete(s.buckets, k)
			}
		}
	}
	b.count++
	s.total++
	switch {
	case status == http.StatusTooManyRequests:
		b.limited++
	case status >= 400:
		b.errors++
	}
	// cap per-bucket sample count so a hammering script can't grow this unbounded
	if len(b.latencies) < 2000 {
		b.latencies = append(b.latencies, float64(dur.Microseconds())/1000.0)
	}
}

type statsPoint struct {
	T     int64 `json:"t"`
	Count int   `json:"count"`
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

func round(f float64, places float64) float64 {
	m := math.Pow(10, places)
	return math.Round(f*m) / m
}

func (s *StatsTracker) Snapshot() map[string]any {
	now := time.Now().Unix()
	prev := now - 1

	s.mu.Lock()
	defer s.mu.Unlock()

	history := make([]statsPoint, 0, statsWindowSeconds)
	for i := int64(statsWindowSeconds - 1); i >= 0; i-- {
		t := now - i
		count := 0
		if b := s.buckets[t]; b != nil {
			count = b.count
		}
		history = append(history, statsPoint{T: t, Count: count})
	}

	var p50, p99, errRate, limitedRate float64
	reqPerSec := 0
	if pb := s.buckets[prev]; pb != nil {
		reqPerSec = pb.count
		lat := append([]float64(nil), pb.latencies...)
		sort.Float64s(lat)
		p50 = percentile(lat, 0.50)
		p99 = percentile(lat, 0.99)
		if pb.count > 0 {
			errRate = float64(pb.errors) / float64(pb.count)
			limitedRate = float64(pb.limited) / float64(pb.count)
		}
	}

	return map[string]any{
		"req_per_sec":  reqPerSec,
		"p50_ms":       round(p50, 2),
		"p99_ms":       round(p99, 2),
		"error_rate":   round(errRate, 4),
		"limited_rate": round(limitedRate, 4),
		"total":        s.total,
		"history":      history,
	}
}

func (s *StatsTracker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Snapshot())
	}
}

// Middleware records every request except the stats endpoint itself.
func (s *StatsTracker) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			h.ServeHTTP(w, r)
			return
		}
		sw := &statusWriter{ResponseWriter: w, code: 200}
		start := time.Now()
		h.ServeHTTP(sw, r)
		s.Record(sw.code, time.Since(start))
	})
}
