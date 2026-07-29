package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testRateLimit() RateLimitConfig {
	return RateLimitConfig{
		CapacityBytes:     1 << 20,
		RefillBytesPerSec: 1 << 20,
		FlatOverheadBytes: 1,
		WriteByteWeight:   1,
		ReadByteWeight:    1,
	}
}

func newTestManager(t *testing.T) *DatabaseManager {
	t.Helper()
	root := t.TempDir()
	man := NewDatabaseManager(root, 1<<20, 1<<30, testRateLimit())
	if err := man.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	return man
}

func getVia(man *DatabaseManager, apiKey, key string) (string, bool, *StoreError) {
	db, serr := man.Lookup(apiKey)
	if serr != nil {
		return "", false, serr
	}
	return db.Get(key)
}

func putVia(man *DatabaseManager, apiKey, key, value string) *StoreError {
	db, serr := man.Lookup(apiKey)
	if serr != nil {
		return serr
	}
	return db.Put(key, value)
}

func deleteVia(man *DatabaseManager, apiKey, key string) *StoreError {
	db, serr := man.Lookup(apiKey)
	if serr != nil {
		return serr
	}
	return db.Delete(key)
}

func rangeVia(man *DatabaseManager, apiKey, start, end string) ([]KV, *StoreError) {
	db, serr := man.Lookup(apiKey)
	if serr != nil {
		return nil, serr
	}
	return db.Range(start, end)
}

func setRateLimitVia(man *DatabaseManager, apiKey string, cfg RateLimitConfig) *StoreError {
	db, serr := man.LookupProvisioned(apiKey)
	if serr != nil {
		return serr
	}
	db.SetRateLimitConfig(cfg)
	return nil
}

// RequestProvision must not touch disk beyond the registry: no per-tenant
// dir or database until the key's first real request.
func TestRequestProvisionDoesNotMaterialize(t *testing.T) {
	man := newTestManager(t)

	key, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}

	man.mu.RLock()
	db, ok := man.tenants[key]
	man.mu.RUnlock()
	if !ok {
		t.Fatal("key should exist after RequestProvision")
	}
	if db.state != tenantPending {
		t.Fatal("key should be pending after RequestProvision")
	}

	dataDirPath := filepath.Join(man.dataRoot, key)
	if _, err := os.Stat(dataDirPath); !os.IsNotExist(err) {
		t.Fatalf("data dir should not exist yet, stat err = %v", err)
	}
}

// The first write against a pending key should materialize its database on
// demand, and the write itself should succeed against the fresh db.
func TestFirstWriteMaterializes(t *testing.T) {
	man := newTestManager(t)

	key, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}

	if serr := putVia(man, key, "foo", "bar"); serr != nil {
		t.Fatalf("Put on pending key: %v", serr)
	}

	man.mu.RLock()
	db, ok := man.tenants[key]
	man.mu.RUnlock()
	if !ok {
		t.Fatal("key should exist")
	}
	if db.state != tenantActive {
		t.Fatal("key should be active after its first write")
	}

	val, ok, serr := getVia(man, key, "foo")
	if serr != nil {
		t.Fatalf("Get: %v", serr)
	}
	if !ok || val != "bar" {
		t.Fatalf("Get = %q, %v, want bar, true", val, ok)
	}
}

// Get/Delete/Range against a never-written key must NOT materialize a
// database: they can answer truthfully (miss / no-op / empty) without one.
func TestReadsDoNotMaterialize(t *testing.T) {
	man := newTestManager(t)

	key, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}

	val, ok, serr := getVia(man, key, "foo")
	if serr != nil {
		t.Fatalf("Get on pending key: %v", serr)
	}
	if ok || val != "" {
		t.Fatalf("Get on pending key = %q, %v, want empty, false", val, ok)
	}

	if serr := deleteVia(man, key, "foo"); serr != nil {
		t.Fatalf("Delete on pending key: %v", serr)
	}

	pairs, serr := rangeVia(man, key, "a", "z")
	if serr != nil {
		t.Fatalf("Range on pending key: %v", serr)
	}
	if len(pairs) != 0 {
		t.Fatalf("Range on pending key = %v, want empty", pairs)
	}

	dataDirPath := filepath.Join(man.dataRoot, key)
	if _, err := os.Stat(dataDirPath); !os.IsNotExist(err) {
		t.Fatalf("data dir should still not exist after only reads, stat err = %v", err)
	}

	man.mu.RLock()
	db, ok := man.tenants[key]
	man.mu.RUnlock()
	if !ok {
		t.Fatal("key should exist")
	}
	if db.state != tenantPending {
		t.Fatal("key should still be pending, not materialized, after only reads")
	}
}

// A Get on a pending key and a Get on a materialized-but-missing key must
// look identical to the client: same result, same error.
func TestPendingMissLooksLikeRealMiss(t *testing.T) {
	man := newTestManager(t)

	pendingKey, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}
	realKey, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}
	// Materialize realKey via an unrelated write, then look up a key that
	// was never stored in it.
	if serr := putVia(man, realKey, "other", "v"); serr != nil {
		t.Fatalf("Put: %v", serr)
	}

	pVal, pOk, pErr := getVia(man, pendingKey, "missing")
	rVal, rOk, rErr := getVia(man, realKey, "missing")
	if pVal != rVal || pOk != rOk || pErr != rErr {
		t.Fatalf("pending miss = (%q, %v, %v), real miss = (%q, %v, %v), want identical", pVal, pOk, pErr, rVal, rOk, rErr)
	}
}

func TestUnknownKeyStillRejected(t *testing.T) {
	man := newTestManager(t)
	if _, _, serr := getVia(man, "not-a-key", "foo"); serr != ErrUnknownAPIKey {
		t.Fatalf("Get with unknown key: %v, want ErrUnknownAPIKey", serr)
	}
}

// A custom rate limit set on a still-pending key applies to its reads right
// away (via the pending bucket) even before it ever materializes: here a
// capacity of exactly one flat-cost unit means the second delete is
// rejected as rate limited rather than allowed.
func TestCustomRateLimitAppliesOnMaterialize(t *testing.T) {
	man := newTestManager(t)

	key, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}

	tight := RateLimitConfig{
		CapacityBytes:     1,
		RefillBytesPerSec: 0,
		FlatOverheadBytes: 1,
		WriteByteWeight:   1,
		ReadByteWeight:    1,
	}
	if serr := setRateLimitVia(man, key, tight); serr != nil {
		t.Fatalf("SetCustomRateLimit: %v", serr)
	}

	if serr := deleteVia(man, key, "whatever"); serr != nil {
		t.Fatalf("first delete (still pending, consumes the pending bucket): %v", serr)
	}
	if serr := deleteVia(man, key, "whatever"); serr != ErrRateLimited {
		t.Fatalf("second delete: %v, want ErrRateLimited (custom capacity=1 exhausted)", serr)
	}
}

// SetCustomRateLimit on an already-materialized key should replace its live
// bucket immediately.
func TestCustomRateLimitAppliesImmediatelyWhenMaterialized(t *testing.T) {
	man := newTestManager(t)

	key, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}
	if serr := putVia(man, key, "warmup", "v"); serr != nil {
		t.Fatalf("materializing put: %v", serr)
	}

	tight := RateLimitConfig{CapacityBytes: 1, RefillBytesPerSec: 0, FlatOverheadBytes: 1, WriteByteWeight: 1, ReadByteWeight: 1}
	if serr := setRateLimitVia(man, key, tight); serr != nil {
		t.Fatalf("SetCustomRateLimit: %v", serr)
	}

	// The new bucket starts full (capacity 1); this consumes it.
	if serr := deleteVia(man, key, "whatever"); serr != nil {
		t.Fatalf("first delete after tightened limit: %v", serr)
	}
	if serr := deleteVia(man, key, "whatever"); serr != ErrRateLimited {
		t.Fatalf("second delete after tightened limit: %v, want ErrRateLimited", serr)
	}
}

func TestSetCustomRateLimitUnknownKey(t *testing.T) {
	man := newTestManager(t)
	if serr := setRateLimitVia(man, "nope", testRateLimit()); serr == nil {
		t.Fatal("expected error setting rate limit on unknown key")
	}
}

// reapInactive should delete a materialized tenant whose activity marker is
// older than maxIdle, and leave a fresh one alone.
func TestReapInactive(t *testing.T) {
	man := newTestManager(t)

	stale, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}
	if serr := putVia(man, stale, "k", "v"); serr != nil {
		t.Fatalf("Put: %v", serr)
	}

	fresh, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}
	if serr := putVia(man, fresh, "k", "v"); serr != nil {
		t.Fatalf("Put: %v", serr)
	}

	// Backdate the stale tenant's in-memory activity clock well past
	// maxIdle -- reapInactive reads Database.lastActive directly now, not
	// the on-disk marker (that's only a restart-recovery snapshot, flushed
	// during reap itself).
	man.mu.RLock()
	staleDB := man.tenants[stale]
	man.mu.RUnlock()
	old := time.Now().Add(-100 * 24 * time.Hour)
	staleDB.lastActive.Store(old.Unix())

	reaped := man.reapInactive(67 * 24 * time.Hour)
	if len(reaped) != 1 || reaped[0] != stale {
		t.Fatalf("reapInactive = %v, want exactly [%s]", reaped, stale)
	}

	man.mu.RLock()
	_, staleStillThere := man.tenants[stale]
	_, freshStillThere := man.tenants[fresh]
	man.mu.RUnlock()
	if staleStillThere {
		t.Fatal("stale tenant should have been deprovisioned")
	}
	if !freshStillThere {
		t.Fatal("fresh tenant should not have been reaped")
	}
}

// Ordinary requests must not touch the activity marker file at all -- that
// would defeat the point of moving lastActive to an in-memory atomic.
// persistActivity only runs from materialize (once) and reapInactive (in
// bulk), never from Get/Put/Delete/Range.
func TestActivityNotWrittenToDiskPerRequest(t *testing.T) {
	man := newTestManager(t)

	key, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}
	if serr := putVia(man, key, "k", "v"); serr != nil {
		t.Fatalf("Put: %v", serr)
	}

	marker := filepath.Join(man.dataRoot, key, LastActiveMarker)
	fi, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("marker should exist after materialize's initial flush: %v", err)
	}
	writtenAt := fi.ModTime()

	// A burst of ordinary requests after materialization shouldn't move the
	// marker's mtime at all -- only reapInactive is allowed to do that.
	for i := 0; i < 20; i++ {
		if serr := putVia(man, key, "k", "v"); serr != nil {
			t.Fatalf("Put: %v", serr)
		}
		if _, _, serr := getVia(man, key, "k"); serr != nil {
			t.Fatalf("Get: %v", serr)
		}
	}

	fi2, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("stat marker: %v", err)
	}
	if !fi2.ModTime().Equal(writtenAt) {
		t.Fatalf("marker mtime changed from %v to %v after ordinary requests -- touchActivity should be in-memory only", writtenAt, fi2.ModTime())
	}
}

// reapInactive flushes each materialized tenant's in-memory activity to its
// marker file, so a subsequent LoadAll (simulating a restart) recovers the
// real idle time instead of resetting the clock to "just started".
func TestReapFlushesActivityForRestartRecovery(t *testing.T) {
	man := newTestManager(t)

	key, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}
	if serr := putVia(man, key, "k", "v"); serr != nil {
		t.Fatalf("Put: %v", serr)
	}

	man.mu.RLock()
	db := man.tenants[key]
	man.mu.RUnlock()
	old := time.Now().Add(-10 * 24 * time.Hour)
	db.lastActive.Store(old.Unix())

	// A reap tick with a maxIdle long enough not to reap anything should
	// still flush the backdated timestamp to disk.
	man.reapInactive(365 * 24 * time.Hour)

	man2 := NewDatabaseManager(man.dataRoot, man.maxValueBytes, man.maxStorageBytes, testRateLimit())
	if err := man2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	man2.mu.RLock()
	restored := man2.tenants[key]
	man2.mu.RUnlock()
	if restored == nil {
		t.Fatal("key should have been restored")
	}
	if restored.state != tenantActive {
		t.Fatal("key should have been restored as active")
	}
	if got := restored.LastActive(); got.Sub(old).Abs() > 2*time.Second {
		t.Fatalf("restored LastActive = %v, want ~%v (recovered from the flushed marker, not reset to now)", got, old)
	}
}

func TestStorageStats(t *testing.T) {
	man := newTestManager(t)

	key, err := man.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}
	if serr := putVia(man, key, "k", "some value bytes"); serr != nil {
		t.Fatalf("Put: %v", serr)
	}

	stats, err := man.StorageStats()
	if err != nil {
		t.Fatalf("StorageStats: %v", err)
	}
	if stats.TenantsUsedBytes == 0 {
		t.Fatal("TenantsUsedBytes should be > 0 after a put")
	}
	if stats.MachineTotalBytes == 0 {
		t.Fatal("MachineTotalBytes should be > 0")
	}
	if stats.MachineFreeBytes > stats.MachineTotalBytes {
		t.Fatalf("MachineFreeBytes (%d) > MachineTotalBytes (%d)", stats.MachineFreeBytes, stats.MachineTotalBytes)
	}
}

// A restart (fresh manager, same dataRoot) should restore a never-materialized
// key as pending, not lose it or wrongly materialize it.
func TestLoadAllRestoresPendingKeys(t *testing.T) {
	root := t.TempDir()
	man1 := NewDatabaseManager(root, 1<<20, 1<<30, testRateLimit())
	if err := man1.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	key, err := man1.RequestProvision()
	if err != nil {
		t.Fatalf("RequestProvision: %v", err)
	}

	man2 := NewDatabaseManager(root, 1<<20, 1<<30, testRateLimit())
	if err := man2.LoadAll(); err != nil {
		t.Fatalf("LoadAll (reload): %v", err)
	}

	man2.mu.RLock()
	db, ok := man2.tenants[key]
	man2.mu.RUnlock()
	if !ok {
		t.Fatal("key should be restored")
	}
	if db.state != tenantPending {
		t.Fatal("never-materialized key should stay pending across reload")
	}
}
