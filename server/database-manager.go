package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var errAlreadyProvisioned = errors.New("already provisioned")

func errNotProvisioned(apiKey string) *StoreError {
	return &StoreError{Status: http.StatusNotFound, Message: fmt.Sprintf("not provisioned: %s", apiKey)}
}

func errInternal(err error) *StoreError {
	return &StoreError{Status: http.StatusInternalServerError, Message: err.Error()}
}

const LastActiveMarker = ".last-active"

type DatabaseManager struct {
	mu              sync.RWMutex
	tenants         map[string]*Database
	dataRoot        string
	maxValueBytes   int
	maxStorageBytes uint64
	rateLimit       RateLimitConfig
}

func NewDatabaseManager(dataRoot string, maxValueBytes int, maxStorageBytes uint64, rateLimit RateLimitConfig) *DatabaseManager {
	return &DatabaseManager{
		tenants:         make(map[string]*Database),
		dataRoot:        dataRoot,
		maxValueBytes:   maxValueBytes,
		maxStorageBytes: maxStorageBytes,
		rateLimit:       rateLimit,
	}
}

func lastPersistedActivity(dataDirPath string) (time.Time, error) {
	file, err := os.Stat(filepath.Join(dataDirPath, LastActiveMarker))
	if err != nil {
		return time.Time{}, err
	}
	return file.ModTime(), nil
}

func (man *DatabaseManager) Lookup(apiKey string) (*Database, *StoreError) {
	man.mu.RLock()
	defer man.mu.RUnlock()
	db, ok := man.tenants[apiKey]
	if !ok {
		return nil, ErrUnknownAPIKey
	}
	return db, nil
}

func (man *DatabaseManager) LookupProvisioned(apiKey string) (*Database, *StoreError) {
	man.mu.RLock()
	defer man.mu.RUnlock()
	db, ok := man.tenants[apiKey]
	if !ok {
		return nil, errNotProvisioned(apiKey)
	}
	return db, nil
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
	for line := range strings.SplitSeq(string(data), "\n") {
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
	return os.MkdirAll(man.dataRoot, 0755)
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
		dataDirPath := filepath.Join(man.dataRoot, apiKey)
		db := NewDatabase(apiKey, dataDirPath, man.maxValueBytes, man.maxStorageBytes, man.rateLimit)
		if _, err := os.Stat(dataDirPath); os.IsNotExist(err) {
			man.tenants[apiKey] = db
			continue
		}
		// TODO persist
		if err := db.InitializeEngine(); err != nil {
			// skip bad keys for now, think of better approach later (never)
			log.Printf("LoadAll: skipping %s, failed to recover: %v", apiKey, err)
			continue
		}
		if last, err := lastPersistedActivity(dataDirPath); err == nil {
			db.lastActive.Store(last.Unix())
		}
		man.tenants[apiKey] = db
	}
	return nil
}

func (man *DatabaseManager) generateAPIKey() (string, error) {
	for range 5 {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		apiKey := hex.EncodeToString(b)

		man.mu.RLock()
		_, exists := man.tenants[apiKey]
		man.mu.RUnlock()
		if exists {
			continue
		}
		return apiKey, nil
	}
	return "", fmt.Errorf("failed to generate unique api key after retries")
}

func (man *DatabaseManager) RequestProvision() (string, error) {
	apiKey, err := man.generateAPIKey()
	if err != nil {
		return "", err
	}

	man.mu.Lock()
	defer man.mu.Unlock()

	if _, exists := man.tenants[apiKey]; exists {
		return "", fmt.Errorf("%w: %s", errAlreadyProvisioned, apiKey)
	}
	if err := man.ensureRoots(); err != nil {
		return "", err
	}
	if err := man.appendToRegistry(apiKey); err != nil {
		return "", err
	}
	dataDirPath := filepath.Join(man.dataRoot, apiKey)
	man.tenants[apiKey] = NewDatabase(apiKey, dataDirPath, man.maxValueBytes, man.maxStorageBytes, man.rateLimit)
	return apiKey, nil
}

func (man *DatabaseManager) Deprovision(apiKey string) *StoreError {
	man.mu.Lock()
	defer man.mu.Unlock()

	db, ok := man.tenants[apiKey]
	if !ok {
		return errNotProvisioned(apiKey)
	}
	if err := db.Destroy(); err != nil {
		return errInternal(err)
	}
	delete(man.tenants, apiKey)
	if err := man.removeFromRegistry(apiKey); err != nil {
		return errInternal(err)
	}
	return nil
}

func (man *DatabaseManager) reapInactive(maxIdle time.Duration) []string {
	man.mu.RLock()
	dbs := make(map[string]*Database, len(man.tenants))
	for key, db := range man.tenants {
		if !db.IsPending() {
			dbs[key] = db
		}
	}
	man.mu.RUnlock()

	now := time.Now()
	var reaped []string
	for key, db := range dbs {
		last := db.LastActive()
		if err := db.PersistActivity(); err != nil {
			log.Printf("reapInactive: failed to persist activity marker for %s: %v", key, err)
		}
		if now.Sub(last) > maxIdle {
			if serr := man.Deprovision(key); serr == nil {
				reaped = append(reaped, key)
			} else {
				log.Printf("reapInactive: failed to deprovision %s: %v", key, serr)
			}
		}
	}
	return reaped
}

func (man *DatabaseManager) StartReaper(interval, maxIdle time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if reaped := man.reapInactive(maxIdle); len(reaped) > 0 {
				log.Printf("reaper: deprovisioned %d inactive tenant(s) idle > %s: %v", len(reaped), maxIdle, reaped)
			}
		}
	}()
}

func (man *DatabaseManager) TenantCount() int {
	man.mu.RLock()
	defer man.mu.RUnlock()
	return len(man.tenants)
}

type StorageStatsResult struct {
	DiskUsedBytes     uint64
	MachineTotalBytes uint64
	MachineFreeBytes  uint64
}

func (man *DatabaseManager) DiskFootprint() (uint64, error) {
	var total uint64
	err := filepath.WalkDir(man.dataRoot, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			total += uint64(st.Blocks) * 512
		} else {
			total += uint64(info.Size())
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}

func (man *DatabaseManager) StorageStats() (StorageStatsResult, error) {
	diskUsed, err := man.DiskFootprint()
	if err != nil {
		return StorageStatsResult{}, err
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(man.dataRoot, &stat); err != nil {
		return StorageStatsResult{}, err
	}
	bsize := uint64(stat.Bsize)

	return StorageStatsResult{
		DiskUsedBytes:     diskUsed,
		MachineTotalBytes: stat.Blocks * bsize,
		MachineFreeBytes:  stat.Bavail * bsize,
	}, nil
}
