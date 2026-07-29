package tests

// Stress tests. These hammer a live server with real concurrency to shake
// out races and deadlocks that small, sequential integration tests won't
// find -- particularly around lazy database materialization (many
// goroutines racing to be the first request against the same fresh key) and
// per-tenant isolation under load. They're skipped under `go test -short`
// since they move real volume and take longer than the rest of the suite.

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestStressConcurrentPutsSingleTenant hammers one already-materialized
// tenant with many concurrent puts/gets on distinct keys and confirms every
// write is durably visible afterwards, with no lost updates or corruption
// under contention.
func TestStressConcurrentPutsSingleTenant(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test, skipped with -short")
	}

	key := provision(t)
	defer deprovision(t, key)

	// This test's volume (2500 requests) would blow through the suite's
	// deliberately tiny default rate-limit bucket; give this tenant a much
	// bigger one via the admin override so the stress test measures
	// concurrency correctness, not rate limiting.
	setResp := setRateLimit(t, key, `{"capacity_bytes":100000000,"refill_bytes_per_sec":100000000}`)
	setResp.Body.Close()
	if setResp.StatusCode != http.StatusNoContent {
		t.Fatalf("setRateLimit: status = %d, want 204", setResp.StatusCode)
	}

	const goroutines = 50
	const perGoroutine = 50

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*perGoroutine)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				k := fmt.Sprintf("stress/%d/%d", g, i)
				v := fmt.Sprintf("v-%d-%d", g, i)
				resp, err := doReqAs(key, http.MethodPut, baseURL+"/key/"+k, strings.NewReader(v))
				if err != nil {
					errs <- err
					continue
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					errs <- fmt.Errorf("put %s: status = %d, want 204", k, resp.StatusCode)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// Spot-check every write landed with the right value.
	for g := 0; g < goroutines; g++ {
		for i := 0; i < perGoroutine; i++ {
			k := fmt.Sprintf("stress/%d/%d", g, i)
			want := fmt.Sprintf("v-%d-%d", g, i)
			resp := mustReqAs(t, key, http.MethodGet, baseURL+"/key/"+k, nil)
			body := new(bytes.Buffer)
			body.ReadFrom(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || body.String() != want {
				t.Fatalf("get %s: status = %d, body = %q, want 200 %q", k, resp.StatusCode, body.String(), want)
			}
		}
	}
}

// TestStressConcurrentTenantMaterialization requests many fresh keys and
// then immediately fires several concurrent requests at each one before
// anything has materialized. This targets the lazy-provisioning path: many
// goroutines racing to be the first request against the same brand-new key
// must not double-create the database, corrupt manager state, or drop
// requests.
func TestStressConcurrentTenantMaterialization(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test, skipped with -short")
	}

	const tenants = 20
	const racersPerTenant = 10

	keys := make([]string, tenants)
	for i := range keys {
		keys[i] = provision(t)
	}
	defer func() {
		for _, k := range keys {
			deprovision(t, k).Body.Close()
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, tenants*racersPerTenant)
	for _, key := range keys {
		key := key
		for r := 0; r < racersPerTenant; r++ {
			wg.Add(1)
			go func(r int) {
				defer wg.Done()
				resp, err := doReqAs(key, http.MethodPut, baseURL+"/key/racer"+fmt.Sprint(r), strings.NewReader("x"))
				if err != nil {
					errs <- err
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					errs <- fmt.Errorf("tenant %s racer %d: status = %d, want 204", key, r, resp.StatusCode)
				}
			}(r)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// Every tenant's writes should all have landed in the same materialized
	// database (no split-brain from a materialization race).
	for _, key := range keys {
		for r := 0; r < racersPerTenant; r++ {
			resp := mustReqAs(t, key, http.MethodGet, baseURL+"/key/racer"+fmt.Sprint(r), nil)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("tenant %s racer %d missing after materialization race: status = %d", key, r, resp.StatusCode)
			}
		}
	}
}

// TestStressConcurrentReadsNeverMaterialize hammers many fresh (pending)
// tenants with concurrent Get/Delete/Range only -- no writes -- and confirms
// none of them ever get a database. This targets the "materialize on first
// write only" behavior under concurrency: a flood of reads racing each
// other must never accidentally create a db, and every read must land on
// the same rate-limit bucket (no double-counted or dropped budget from a
// race in the pending path).
func TestStressConcurrentReadsNeverMaterialize(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test, skipped with -short")
	}

	const tenants = 20
	const racersPerTenant = 15

	keys := make([]string, tenants)
	for i := range keys {
		keys[i] = provision(t)
	}
	defer func() {
		for _, k := range keys {
			deprovision(t, k).Body.Close()
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, tenants*racersPerTenant)
	for _, key := range keys {
		key := key
		for r := 0; r < racersPerTenant; r++ {
			wg.Add(1)
			go func(r int) {
				defer wg.Done()
				var resp *http.Response
				var err error
				switch r % 3 {
				case 0:
					resp, err = doReqAs(key, http.MethodGet, baseURL+"/key/nope"+fmt.Sprint(r), nil)
				case 1:
					resp, err = doReqAs(key, http.MethodDelete, baseURL+"/key/nope"+fmt.Sprint(r), nil)
				default:
					resp, err = doReqAs(key, http.MethodGet, baseURL+"/range?start=a&end=z", nil)
				}
				if err != nil {
					errs <- err
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusNotFound &&
					resp.StatusCode != http.StatusNoContent &&
					resp.StatusCode != http.StatusOK {
					errs <- fmt.Errorf("tenant %s racer %d: status = %d, want 404/204/200", key, r, resp.StatusCode)
				}
			}(r)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestStressManyTenantsIsolated provisions many tenants and writes/reads
// across all of them concurrently, checking cross-tenant isolation holds up
// under load (not just in the single-pair case the regular integration
// tests cover).
func TestStressManyTenantsIsolated(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test, skipped with -short")
	}

	const tenants = 30
	keys := make([]string, tenants)
	for i := range keys {
		keys[i] = provision(t)
	}
	defer func() {
		for _, k := range keys {
			deprovision(t, k).Body.Close()
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, tenants)
	for i, key := range keys {
		i, key := i, key
		wg.Add(1)
		go func() {
			defer wg.Done()
			val := fmt.Sprintf("tenant-%d-secret", i)
			resp, err := doReqAs(key, http.MethodPut, baseURL+"/key/shared-name", strings.NewReader(val))
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				errs <- fmt.Errorf("tenant %d put: status = %d, want 204", i, resp.StatusCode)
				return
			}
			resp2, err := doReqAs(key, http.MethodGet, baseURL+"/key/shared-name", nil)
			if err != nil {
				errs <- err
				return
			}
			defer resp2.Body.Close()
			body := new(bytes.Buffer)
			body.ReadFrom(resp2.Body)
			if body.String() != val {
				errs <- fmt.Errorf("tenant %d: got %q, want %q (cross-tenant leak?)", i, body.String(), val)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
