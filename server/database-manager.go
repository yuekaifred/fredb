package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var errAlreadyProvisioned = errors.New("already provisioned")

func errNotProvisioned(apiKey string) *StoreError {
	return &StoreError{Status: http.StatusNotFound, Message: fmt.Sprintf("not provisioned: %s", apiKey)}
}

func errInternal(err error) *StoreError {
	return &StoreError{Status: http.StatusInternalServerError, Message: err.Error()}
}

type DatabaseManager struct {
	mu              sync.RWMutex
	dbs             map[string]*Database
	buckets         map[string]*TokenBucket
	dataRoot        string
	sockRoot        string
	maxValueBytes   int
	maxStorageBytes uint64
	rateLimit       RateLimitConfig
}

func NewDatabaseManager(dataRoot, sockRoot string, maxValueBytes int, maxStorageBytes uint64, rateLimit RateLimitConfig) *DatabaseManager {
	return &DatabaseManager{
		dbs:             make(map[string]*Database),
		buckets:         make(map[string]*TokenBucket),
		dataRoot:        dataRoot,
		sockRoot:        sockRoot,
		maxValueBytes:   maxValueBytes,
		maxStorageBytes: maxStorageBytes,
		rateLimit:       rateLimit,
	}
}

func withDB[T any](man *DatabaseManager, apiKey string, cost float64, fn func(*Database) (T, *StoreError)) (T, *StoreError) {
	man.mu.RLock()
	db, ok := man.dbs[apiKey]
	bucket := man.buckets[apiKey]
	man.mu.RUnlock()

	var zero T
	if !ok {
		return zero, ErrUnknownAPIKey
	}
	if bucket != nil && !bucket.Consume(cost) {
		return zero, ErrRateLimited
	}
	return fn(db)
}

func (man *DatabaseManager) chargeBytes(apiKey string, n float64) {
	man.mu.RLock()
	bucket := man.buckets[apiKey]
	man.mu.RUnlock()
	if bucket != nil {
		bucket.Charge(n)
	}
}

type getResult struct {
	val   string
	found bool
}

func (man *DatabaseManager) Get(apiKey, key string) (string, bool, *StoreError) {
	r, err := withDB(man, apiKey, man.rateLimit.FlatCost(), func(db *Database) (getResult, *StoreError) {
		val, found, err := db.Get(key)
		return getResult{val, found}, err
	})
	if err == nil && r.found {
		man.chargeBytes(apiKey, man.rateLimit.ReadCost(len(r.val)))
	}
	return r.val, r.found, err
}

func (man *DatabaseManager) Put(apiKey, key, value string) *StoreError {
	if len(value) > man.maxValueBytes {
		return ErrTooLarge
	}
	_, err := withDB(man, apiKey, man.rateLimit.PutCost(len(value)), func(db *Database) (struct{}, *StoreError) {
		return struct{}{}, db.Put(key, value)
	})
	return err
}

func (man *DatabaseManager) Delete(apiKey, key string) *StoreError {
	_, err := withDB(man, apiKey, man.rateLimit.FlatCost(), func(db *Database) (struct{}, *StoreError) {
		return struct{}{}, db.Delete(key)
	})
	return err
}

func (man *DatabaseManager) Range(apiKey, start, end string) ([]KV, *StoreError) {
	pairs, err := withDB(man, apiKey, man.rateLimit.FlatCost(), func(db *Database) ([]KV, *StoreError) {
		return db.Range(start, end)
	})
	if err == nil {
		total := 0
		for _, kv := range pairs {
			total += len(kv.Key) + len(kv.Value)
		}
		man.chargeBytes(apiKey, man.rateLimit.ReadCost(total))
	}
	return pairs, err
}

func (man *DatabaseManager) registryPath() string {
	return filepath.Join(man.dataRoot, "keys.txt")
}

func (man *DatabaseManager) readRegistry() ([]string, error) {
	data, err := os.ReadFile(man.registryPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			keys = append(keys, line)
		}
	}
	return keys, nil
}

func (man *DatabaseManager) appendToRegistry(apiKey string) error {
	f, err := os.OpenFile(man.registryPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(apiKey + "\n")
	return err
}

// should probably find another way of keeping the registry
func (man *DatabaseManager) removeFromRegistry(apiKey string) error {
	keys, err := man.readRegistry()
	if err != nil {
		return err
	}
	kept := keys[:0]
	for _, k := range keys {
		if k != apiKey {
			kept = append(kept, k)
		}
	}
	content := ""
	if len(kept) > 0 {
		content = strings.Join(kept, "\n") + "\n"
	}
	tmp := man.registryPath() + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, man.registryPath())
}

func (man *DatabaseManager) ensureRoots() error {
	if err := os.MkdirAll(man.dataRoot, 0755); err != nil {
		return err
	}
	return os.MkdirAll(man.sockRoot, 0755)
}

func (man *DatabaseManager) LoadAll() error {
	man.mu.Lock()
	defer man.mu.Unlock()

	if err := man.ensureRoots(); err != nil {
		return err
	}
	keys, err := man.readRegistry()
	if err != nil {
		return err
	}
	for _, apiKey := range keys {
		sockPath := filepath.Join(man.sockRoot, apiKey+".sock")
		dataDirPath := filepath.Join(man.dataRoot, apiKey)
		db, err := NewDatabase(apiKey, sockPath, dataDirPath, man.maxValueBytes, man.maxStorageBytes)
		if err != nil {
			// skip bad keys for now, think of better approach later (never)
			log.Printf("LoadAll: skipping %s, failed to recover: %v", apiKey, err)
			continue
		}
		man.dbs[apiKey] = db
		man.buckets[apiKey] = NewTokenBucket(man.rateLimit)
	}
	return nil
}

func (man *DatabaseManager) generateAPIKey() (string, error) {
	for i := 0; i < 5; i++ {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		apiKey := hex.EncodeToString(b)

		man.mu.RLock()
		_, exists := man.dbs[apiKey]
		man.mu.RUnlock()
		if exists {
			continue
		}
		return apiKey, nil
	}
	return "", fmt.Errorf("failed to generate unique api key after retries")
}

func (man *DatabaseManager) Provision() (string, error) {
	apiKey, err := man.generateAPIKey()
	if err != nil {
		return "", err
	}

	man.mu.Lock()
	defer man.mu.Unlock()

	if _, exists := man.dbs[apiKey]; exists {
		return "", fmt.Errorf("%w: %s", errAlreadyProvisioned, apiKey)
	}
	if err := man.ensureRoots(); err != nil {
		return "", err
	}
	if err := man.appendToRegistry(apiKey); err != nil {
		return "", err
	}

	sockPath := filepath.Join(man.sockRoot, apiKey+".sock")
	dataDirPath := filepath.Join(man.dataRoot, apiKey)

	db, err := NewDatabase(apiKey, sockPath, dataDirPath, man.maxValueBytes, man.maxStorageBytes)
	if err != nil {
		man.removeFromRegistry(apiKey)
		return "", err
	}
	man.dbs[apiKey] = db
	man.buckets[apiKey] = NewTokenBucket(man.rateLimit)
	return apiKey, nil
}

func (man *DatabaseManager) Deprovision(apiKey string) *StoreError {
	man.mu.Lock()
	defer man.mu.Unlock()

	db, ok := man.dbs[apiKey]
	if !ok {
		return errNotProvisioned(apiKey)
	}
	if err := db.DestroyCompletely(); err != nil {
		return errInternal(err)
	}
	delete(man.dbs, apiKey)
	delete(man.buckets, apiKey)
	if err := man.removeFromRegistry(apiKey); err != nil {
		return errInternal(err)
	}
	return nil
}

func (man *DatabaseManager) Pause(apiKey string) *StoreError {
	man.mu.Lock()
	defer man.mu.Unlock()

	db, ok := man.dbs[apiKey]
	if !ok {
		return errNotProvisioned(apiKey)
	}
	if err := db.Pause(); err != nil {
		return errInternal(err)
	}
	return nil
}

func (man *DatabaseManager) Resume(apiKey string) *StoreError {
	man.mu.Lock()
	defer man.mu.Unlock()

	db, ok := man.dbs[apiKey]
	if !ok {
		return errNotProvisioned(apiKey)
	}
	if err := db.Resume(); err != nil {
		return errInternal(err)
	}
	return nil
}
