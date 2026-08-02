package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type StoreError struct {
	Status  int
	Message string
}

func (e *StoreError) Error() string   { return e.Message }
func (e *StoreError) StatusCode() int { return e.Status }

func errUnavailable(err error) *StoreError {
	return &StoreError{Status: http.StatusServiceUnavailable, Message: err.Error()}
}

var (
	ErrTooLarge         = &StoreError{Status: http.StatusRequestEntityTooLarge, Message: "value too large"}
	errStorageFull      = &StoreError{Status: http.StatusInsufficientStorage, Message: "storage limit exceeded"}
	ErrRateLimited      = &StoreError{Status: http.StatusTooManyRequests, Message: "rate limit exceeded"}
	errEmptyKey         = &StoreError{Status: http.StatusBadRequest, Message: "missing key"}
	errEmptyRangeBounds = &StoreError{Status: http.StatusBadRequest, Message: "start and end required"}
	ErrUnknownAPIKey    = &StoreError{Status: http.StatusUnauthorized, Message: "unknown api key"}
	errGone             = &StoreError{Status: http.StatusServiceUnavailable, Message: "database unavailable"}
)

type tenantState int

const (
	tenantPending tenantState = iota
	tenantActive
)

type Database struct {
	APIKey      string
	dataDirPath string
	mu          sync.RWMutex

	maxValueBytes   int
	maxStorageBytes uint64
	bucket          *TokenBucket
	state           tenantState
	lastActive      atomic.Int64
	rateLimit       RateLimitConfig

	engine *CgoEngine
}

func NewDatabase(apiKey, dataDirPath string, maxValueBytes int, maxStorageBytes uint64, rateLimit RateLimitConfig) *Database {
	db := &Database{
		APIKey:          apiKey,
		dataDirPath:     dataDirPath,
		maxValueBytes:   maxValueBytes,
		maxStorageBytes: maxStorageBytes,
		bucket:          NewTokenBucket(rateLimit),
		rateLimit:       rateLimit,
		// defaults to tenantPending btw
	}
	db.TouchActivity()
	return db
}

func (db *Database) TouchActivity() {
	db.lastActive.Store(time.Now().Unix())
}

func (db *Database) LastActive() time.Time {
	return time.Unix(db.lastActive.Load(), 0)
}

// hm just read property?
func (db *Database) IsPending() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.state == tenantPending
}

func (db *Database) InitializeEngine() error {
	engine, err := OpenCgoEngine(db.dataDirPath)
	if err != nil {
		return err
	}
	db.engine = engine
	db.state = tenantActive
	return nil
}

func (db *Database) PersistActivity() error {
	marker := filepath.Join(db.dataDirPath, LastActiveMarker)
	if err := os.WriteFile(marker, nil, 0644); err != nil {
		return err
	}
	last := db.LastActive()
	return os.Chtimes(marker, last, last)
}

func (db *Database) materialize() error {
	if err := os.MkdirAll(db.dataDirPath, 0755); err != nil {
		return err
	}
	if err := db.InitializeEngine(); err != nil {
		return err
	}
	if err := db.PersistActivity(); err != nil {
		log.Printf("materialize: failed to write initial activity marker for %s: %v", db.APIKey, err)
	}
	return nil
}

func (db *Database) Destroy() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.engine != nil {
		db.engine.Close()
		db.engine = nil
	}
	return os.RemoveAll(db.dataDirPath)
}

func (db *Database) SetRateLimitConfig(cfg RateLimitConfig) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.rateLimit = cfg
	db.bucket = NewTokenBucket(cfg)
}

func (db *Database) Get(key string) (string, bool, *StoreError) {
	if key == "" {
		return "", false, errEmptyKey
	}
	db.mu.RLock()
	bucket, rl, engine, state := db.bucket, db.rateLimit, db.engine, db.state
	db.mu.RUnlock()

	if bucket != nil && !bucket.Consume(rl.FlatCost()) {
		return "", false, ErrRateLimited
	}
	if state == tenantPending {
		// always a miss when pending
		return "", false, nil
	}
	db.TouchActivity()
	if engine == nil {
		return "", false, errGone
	}
	val, found, err := engine.Get(key)
	if err != nil {
		return "", false, errUnavailable(err)
	}
	if found && bucket != nil {
		bucket.Charge(rl.ReadCost(len(val)))
	}
	return val, found, nil
}

func (db *Database) Put(key, value string) *StoreError {
	if key == "" {
		return errEmptyKey
	}
	if len(value) > db.maxValueBytes {
		return ErrTooLarge
	}

	db.mu.Lock()
	if db.state == tenantPending {
		// lazy
		if err := db.materialize(); err != nil {
			db.mu.Unlock()
			return errInternal(err)
		}
	}
	bucket, rl, engine := db.bucket, db.rateLimit, db.engine
	db.mu.Unlock()

	if bucket != nil && !bucket.Consume(rl.PutCost(len(value))) {
		return ErrRateLimited
	}
	db.TouchActivity()
	if engine == nil {
		return errGone
	}
	size, err := engine.TotalSize()
	if err != nil {
		return errUnavailable(err)
	}
	if size >= db.maxStorageBytes {
		return errStorageFull
	}
	if err := engine.Put(key, value); err != nil {
		return errUnavailable(err)
	}
	return nil
}

func (db *Database) Delete(key string) *StoreError {
	if key == "" {
		return errEmptyKey
	}
	db.mu.RLock()
	bucket, rl, engine, state := db.bucket, db.rateLimit, db.engine, db.state
	db.mu.RUnlock()

	if bucket != nil && !bucket.Consume(rl.FlatCost()) {
		return ErrRateLimited
	}
	if state == tenantPending {
		return nil
	}
	db.TouchActivity()
	if engine == nil {
		return errGone
	}
	if err := engine.Delete(key); err != nil {
		return errUnavailable(err)
	}
	return nil
}

type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (db *Database) Range(start, end string) ([]KV, *StoreError) {
	if start == "" || end == "" {
		return nil, errEmptyRangeBounds
	}
	db.mu.RLock()
	bucket, rl, engine, state := db.bucket, db.rateLimit, db.engine, db.state
	db.mu.RUnlock()

	if bucket != nil && !bucket.Consume(rl.FlatCost()) {
		return nil, ErrRateLimited
	}
	if state == tenantPending {
		return nil, nil
	}
	db.TouchActivity()
	if engine == nil {
		return nil, errGone
	}
	pairs, err := engine.Range(start, end)
	if err != nil {
		return nil, errUnavailable(err)
	}
	if bucket != nil {
		total := 0
		for _, kv := range pairs {
			total += len(kv.Key) + len(kv.Value)
		}
		bucket.Charge(rl.ReadCost(total))
	}
	return pairs, nil
}
