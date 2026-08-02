package main

import (
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	website "fredb-website"
)

const (
	statusPublishInterval = 1 * time.Second
	statusHealthInterval  = 5 * time.Second
	statusLatWindow       = 4096
	statusFeedSize        = 12
)

type latRing struct {
	buf  []time.Duration
	next int
	full bool
}

func newLatRing(n int) *latRing { return &latRing{buf: make([]time.Duration, n)} }

func (r *latRing) add(d time.Duration) {
	r.buf[r.next] = d
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

func (r *latRing) percentile(p float64) (time.Duration, bool) {
	n := r.next
	if r.full {
		n = len(r.buf)
	}
	if n == 0 {
		return 0, false
	}
	s := make([]time.Duration, n)
	copy(s, r.buf[:n])
	slices.Sort(s)
	idx := int(p / 100 * float64(n-1))
	return s[idx], true
}

type opRecord struct {
	when    time.Time
	op      string
	latency time.Duration
	ok      bool
}

type StatusSampler struct {
	manager   *DatabaseManager
	startTime time.Time

	mu          sync.Mutex
	diskUsed    uint64
	writeLat    *latRing
	readLat     *latRing
	totalOps    uint64
	recentOps   []opRecord
	lastOK      bool
	opsPerSec   float64
	lastOpsTime time.Time
	lastOpsCnt  uint64

	snapshot atomic.Pointer[website.StatusView]
}

func NewStatusSampler(manager *DatabaseManager) *StatusSampler {
	return &StatusSampler{
		manager:     manager,
		startTime:   time.Now(),
		writeLat:    newLatRing(statusLatWindow),
		readLat:     newLatRing(statusLatWindow),
		lastOK:      true,
		lastOpsTime: time.Now(),
	}
}

func (s *StatusSampler) Start() {
	go s.loop()
}

func (s *StatusSampler) loop() {
	s.sampleHealth()
	s.publish()
	ticker := time.NewTicker(statusPublishInterval)
	defer ticker.Stop()
	lastHealth := time.Now()
	for range ticker.C {
		if time.Since(lastHealth) >= statusHealthInterval {
			s.sampleHealth()
			lastHealth = time.Now()
		}
		s.publish()
	}
}

func (s *StatusSampler) publish() {
	v := s.buildView()
	s.snapshot.Store(&v)
}

func (s *StatusSampler) Measure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op, isWrite, track := classifyRequest(r)
		if !track {
			next.ServeHTTP(w, r)
			return
		}
		sw := &statusWriter{ResponseWriter: w, code: 200}
		t0 := time.Now()
		next.ServeHTTP(sw, r)
		s.record(op, isWrite, time.Since(t0), sw.code < 400)
	})
}

func classifyRequest(r *http.Request) (op string, isWrite, track bool) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/key/"):
		switch r.Method {
		case http.MethodGet:
			return "GET", false, true
		case http.MethodPut:
			return "PUT", true, true
		case http.MethodDelete:
			return "DELETE", true, true
		}
	case r.URL.Path == "/range" && r.Method == http.MethodGet:
		return "RANGE", false, true
	}
	return "", false, false
}

func (s *StatusSampler) record(op string, isWrite bool, d time.Duration, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if isWrite {
		s.writeLat.add(d)
	} else {
		s.readLat.add(d)
	}
	s.totalOps++
	s.lastOK = ok
	rec := opRecord{when: time.Now(), op: op, latency: d, ok: ok}
	if len(s.recentOps) < statusFeedSize {
		s.recentOps = append(s.recentOps, rec)
	} else {
		copy(s.recentOps, s.recentOps[1:])
		s.recentOps[len(s.recentOps)-1] = rec
	}
}

func (s *StatusSampler) sampleHealth() {
	stats, err := s.manager.StorageStats()
	if err != nil {
		log.Printf("status: StorageStats failed: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.diskUsed = stats.DiskUsedBytes
	}
	now := time.Now()
	if elapsed := now.Sub(s.lastOpsTime).Seconds(); elapsed > 0 {
		s.opsPerSec = float64(s.totalOps-s.lastOpsCnt) / elapsed
		s.lastOpsCnt = s.totalOps
		s.lastOpsTime = now
	}
}

func (s *StatusSampler) Status() website.StatusView {
	if v := s.snapshot.Load(); v != nil {
		return *v
	}
	return website.StatusView{}
}

func (s *StatusSampler) buildView() website.StatusView {
	tenants := s.manager.TenantCount()

	s.mu.Lock()
	defer s.mu.Unlock()

	ops := make([]website.StatusOp, 0, len(s.recentOps))
	for i := len(s.recentOps) - 1; i >= 0; i-- {
		r := s.recentOps[i]
		ops = append(ops, website.StatusOp{
			When:    r.when.Format("15:04:05"),
			Op:      r.op,
			Latency: humanLatency(r.latency),
			OK:      r.ok,
		})
	}

	return website.StatusView{
		Healthy:     s.lastOK,
		TenantCount: fmt.Sprintf("%d", tenants),
		DiskUsed:    humanBytes(s.diskUsed),
		Uptime:      humanUptime(time.Since(s.startTime)),
		WriteP50:    percentileText(s.writeLat, 50),
		WriteP99:    percentileText(s.writeLat, 99),
		ReadP50:     percentileText(s.readLat, 50),
		ReadP99:     percentileText(s.readLat, 99),
		OpsPerSec:   fmt.Sprintf("%.0f", s.opsPerSec),
		TotalOps:    humanCount(s.totalOps),
		RecentOps:   ops,
	}
}

func percentileText(r *latRing, p float64) string {
	d, ok := r.percentile(p)
	if !ok {
		return "—"
	}
	return humanLatency(d)
}

func humanLatency(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1e3)
	default:
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	}
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func humanCount(n uint64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	}
}

func humanUptime(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	d -= time.Duration(m) * time.Minute
	sec := int(d / time.Second)
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, h)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, sec)
	}
	return fmt.Sprintf("%ds", sec)
}
