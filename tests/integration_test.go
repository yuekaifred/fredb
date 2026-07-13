package tests

// Integration tests. These talk real HTTP to a real fredb-server backed by a
// real engine-server, listening on FREDB_TEST_BASE_URL (default
// http://localhost:8080) for the api port and FREDB_TEST_ADMIN_URL (default
// http://localhost:8081) for the admin port. They do not start the server
// themselves — that's the job of `run-integration-tests.sh` (or `nix run
// .#tests`). TestMain creates its own api key against the admin port and
// sends it as X-Api-Key on every request. Run standalone with:
//
//	FREDB_TEST_BASE_URL=http://localhost:8080 FREDB_TEST_ADMIN_URL=http://localhost:8081 go test ./... -v

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// maxValueBytes mirrors server/main.go's own constant. Kept in sync by hand
// since these tests only ever talk to the server over HTTP, not its
// internals — if the server's limit changes, TestValueTooLarge needs a
// matching update.
const maxValueBytes = 50 * 1024 * 1024 // 50MB

// maxStorageBytes mirrors run-integration-tests.sh's FREDB_MAX_STORAGE_BYTES
// (and flake.nix's tests package). Kept small on purpose so the storage-limit
// tests dont need to write real gigabytes through the actual engine.
const maxStorageBytes = 256 * 1024 // 256KB

type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var (
	baseURL  string
	adminURL string
	apiKey   string
)

func TestMain(m *testing.M) {
	baseURL = os.Getenv("FREDB_TEST_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	adminURL = os.Getenv("FREDB_TEST_ADMIN_URL")
	if adminURL == "" {
		adminURL = "http://localhost:8081"
	}

	resp, err := http.Post(adminURL+"/keys", "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no admin server reachable at %s: %v\n", adminURL, err)
		fmt.Fprintln(os.Stderr, "run this via `./run-integration-tests.sh`, or start the server yourself and set FREDB_TEST_BASE_URL / FREDB_TEST_ADMIN_URL")
		os.Exit(1)
	}
	var created struct {
		APIKey string `json:"api_key"`
	}
	err = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if err != nil || created.APIKey == "" {
		fmt.Fprintf(os.Stderr, "failed to create test api key: %v\n", err)
		os.Exit(1)
	}
	apiKey = created.APIKey

	resp, err = doReq(http.MethodGet, baseURL+"/key/__integration_test_healthcheck__", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no server reachable at %s: %v\n", baseURL, err)
		os.Exit(1)
	}
	resp.Body.Close()
	os.Exit(m.Run())
}

// doReqAs builds a request against the api port with the given X-Api-Key,
// then sends it.
func doReqAs(key, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", key)
	return http.DefaultClient.Do(req)
}

// doReq is doReqAs against the shared test api key. Every ordinary test call
// goes through this instead of http.Get/Post so the header is never
// forgotten.
func doReq(method, url string, body io.Reader) (*http.Response, error) {
	return doReqAs(apiKey, method, url, body)
}

func mustReq(t *testing.T, method, url string, body io.Reader) *http.Response {
	t.Helper()
	resp, err := doReq(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func mustReqAs(t *testing.T, key, method, url string, body io.Reader) *http.Response {
	t.Helper()
	resp, err := doReqAs(key, method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// testKey returns a key namespaced to this test run and test name, so
// concurrent/repeated runs against the same real store never collide.
func testKey(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("itest/%s/%d/%s", t.Name(), time.Now().UnixNano(), suffix)
}

func del(t *testing.T, key string) {
	t.Helper()
	resp := mustReq(t, http.MethodDelete, baseURL+"/key/"+key, nil)
	resp.Body.Close()
}

// provision creates a fresh api key via the admin port. Callers own its
// lifecycle and should deprovision it (directly, or via defer).
func provision(t *testing.T) string {
	t.Helper()
	resp, err := http.Post(adminURL+"/keys", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /keys status = %d, want 200", resp.StatusCode)
	}
	var created struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.APIKey == "" {
		t.Fatal("POST /keys returned empty api_key")
	}
	return created.APIKey
}

func deprovision(t *testing.T, key string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, adminURL+"/keys/"+key, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func pause(t *testing.T, key string) *http.Response {
	t.Helper()
	resp, err := http.Post(adminURL+"/keys/"+key+"/pause", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func resume(t *testing.T, key string) *http.Response {
	t.Helper()
	resp, err := http.Post(adminURL+"/keys/"+key+"/resume", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A1
func TestPutThenGetRoundtrip(t *testing.T) {
	key := testKey(t, "foo")
	defer del(t, key)

	resp := mustReq(t, http.MethodPut, baseURL+"/key/"+key, strings.NewReader("bar"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", resp.StatusCode)
	}

	resp = mustReq(t, http.MethodGet, baseURL+"/key/"+key, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	if body.String() != "bar" {
		t.Fatalf("GET body = %q, want %q", body.String(), "bar")
	}
}

// A2
func TestGetMissingKey(t *testing.T) {
	key := testKey(t, "nope")

	resp := mustReq(t, http.MethodGet, baseURL+"/key/"+key, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// A3
func TestDeleteRemovesKey(t *testing.T) {
	key := testKey(t, "foo")

	resp := mustReq(t, http.MethodPut, baseURL+"/key/"+key, strings.NewReader("bar"))
	resp.Body.Close()

	resp = mustReq(t, http.MethodDelete, baseURL+"/key/"+key, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}

	resp = mustReq(t, http.MethodGet, baseURL+"/key/"+key, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete status = %d, want 404", resp.StatusCode)
	}
}

// A4
func TestDeleteMissingKey(t *testing.T) {
	key := testKey(t, "nope")

	resp := mustReq(t, http.MethodDelete, baseURL+"/key/"+key, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

// A5
func TestEmptyKeyRejected(t *testing.T) {
	resp := mustReq(t, http.MethodGet, baseURL+"/key/", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// A6
func TestMethodNotAllowed(t *testing.T) {
	key := testKey(t, "foo")

	resp := mustReq(t, http.MethodPost, baseURL+"/key/"+key, strings.NewReader("x"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

// A7
func TestEmptyValueAllowed(t *testing.T) {
	key := testKey(t, "foo")
	defer del(t, key)

	resp := mustReq(t, http.MethodPut, baseURL+"/key/"+key, strings.NewReader(""))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", resp.StatusCode)
	}

	resp = mustReq(t, http.MethodGet, baseURL+"/key/"+key, nil)
	defer resp.Body.Close()
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	if body.String() != "" {
		t.Fatalf("GET body = %q, want empty", body.String())
	}
}

// A8
func TestBinaryValueSurvives(t *testing.T) {
	key := testKey(t, "foo")
	defer del(t, key)

	val := "a\nb\r\x00c"
	resp := mustReq(t, http.MethodPut, baseURL+"/key/"+key, strings.NewReader(val))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", resp.StatusCode)
	}

	resp = mustReq(t, http.MethodGet, baseURL+"/key/"+key, nil)
	defer resp.Body.Close()
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	if body.String() != val {
		t.Fatalf("GET body = %q, want %q", body.String(), val)
	}
}

// A9
func TestKeyWithSlashes(t *testing.T) {
	key := testKey(t, "a/b/c")
	defer del(t, key)

	resp := mustReq(t, http.MethodPut, baseURL+"/key/"+key, strings.NewReader("v"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204", resp.StatusCode)
	}

	resp = mustReq(t, http.MethodGet, baseURL+"/key/"+key, nil)
	defer resp.Body.Close()
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	if body.String() != "v" {
		t.Fatalf("GET body = %q, want v", body.String())
	}
}

// A10
func TestValueTooLarge(t *testing.T) {
	key := testKey(t, "big")

	big := bytes.Repeat([]byte("x"), maxValueBytes+1)
	resp := mustReq(t, http.MethodPut, baseURL+"/key/"+key, bytes.NewReader(big))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

// TestStorageLimitEnforced provisions its own tenant (isolated from every
// other test's data, since maxStorageBytes is a small shared cap) and writes
// chunks until the server starts rejecting puts with 507.
func TestStorageLimitEnforced(t *testing.T) {
	key := provision(t)
	defer deprovision(t, key)

	chunk := bytes.Repeat([]byte("x"), 50*1024) // 50KB, several fit under the 256KB cap
	var last *http.Response
	for i := 0; i < 10; i++ {
		resp, err := doReqAs(key, http.MethodPut, baseURL+"/key/chunk"+fmt.Sprint(i), bytes.NewReader(chunk))
		if err != nil {
			t.Fatal(err)
		}
		last = resp
		resp.Body.Close()
		if resp.StatusCode == http.StatusInsufficientStorage {
			break
		}
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("put chunk%d: status = %d, want 204", i, resp.StatusCode)
		}
	}
	if last.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("expected eventual 507 storage full, last status = %d", last.StatusCode)
	}
}

// TestStorageLimitDoesNotBlockReads fills a tenant's storage cap, then checks
// that gets/deletes still work — only writes should be rejected once full.
func TestStorageLimitDoesNotBlockReads(t *testing.T) {
	key := provision(t)
	defer deprovision(t, key)

	chunk := bytes.Repeat([]byte("x"), 50*1024)
	resp := mustReqAs(t, key, http.MethodPut, baseURL+"/key/seed", bytes.NewReader(chunk))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("seed put: status = %d, want 204", resp.StatusCode)
	}

	for i := 0; i < 10; i++ {
		resp, err := doReqAs(key, http.MethodPut, baseURL+"/key/chunk"+fmt.Sprint(i), bytes.NewReader(chunk))
		if err != nil {
			t.Fatal(err)
		}
		full := resp.StatusCode == http.StatusInsufficientStorage
		resp.Body.Close()
		if full {
			break
		}
	}

	resp = mustReqAs(t, key, http.MethodGet, baseURL+"/key/seed", nil)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != string(chunk) {
		t.Fatalf("get after full: status = %d, body len = %d, want 200 and original chunk", resp.StatusCode, len(body))
	}

	resp2 := mustReqAs(t, key, http.MethodDelete, baseURL+"/key/seed", nil)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("delete after full: status = %d, want 204", resp2.StatusCode)
	}
}

// A12
func TestRangeHappyPath(t *testing.T) {
	prefix := testKey(t, "")
	keyA := prefix + "a"
	keyB := prefix + "b"
	defer del(t, keyA)
	defer del(t, keyB)

	for _, kv := range []KV{{Key: keyA, Value: "1"}, {Key: keyB, Value: "2"}} {
		resp := mustReq(t, http.MethodPut, baseURL+"/key/"+kv.Key, strings.NewReader(kv.Value))
		resp.Body.Close()
	}

	resp := mustReq(t, http.MethodGet, baseURL+"/range?start="+prefix+"a&end="+prefix+"z", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var got []KV
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := []KV{{Key: keyA, Value: "1"}, {Key: keyB, Value: "2"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// A13
func TestRangeMissingParams(t *testing.T) {
	resp := mustReq(t, http.MethodGet, baseURL+"/range", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	resp = mustReq(t, http.MethodGet, baseURL+"/range?start=a", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// A14
func TestRangeWrongMethod(t *testing.T) {
	resp := mustReq(t, http.MethodPut, baseURL+"/range?start=a&end=z", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

// A15
func TestRangeEmptyResult(t *testing.T) {
	prefix := testKey(t, "")

	resp := mustReq(t, http.MethodGet, baseURL+"/range?start="+prefix+"a&end="+prefix+"a", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []KV
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want zero-length slice", got)
	}
}

// A16
func TestCORSHeadersOnAPI(t *testing.T) {
	key := testKey(t, "foo")

	resp := mustReq(t, http.MethodGet, baseURL+"/key/"+key, nil)
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}

	// OPTIONS must not create the key.
	resp = mustReq(t, http.MethodOptions, baseURL+"/key/"+key, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}

	resp = mustReq(t, http.MethodGet, baseURL+"/key/"+key, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after OPTIONS status = %d, want 404 (OPTIONS must not touch the store)", resp.StatusCode)
	}
}

// B1
func TestMissingAPIKeyRejected(t *testing.T) {
	resp, err := http.Get(baseURL + "/key/" + testKey(t, "x"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// B2
func TestUnknownAPIKeyRejected(t *testing.T) {
	resp, err := doReqAs("not-a-provisioned-key", http.MethodGet, baseURL+"/key/"+testKey(t, "x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// B3
func TestUnknownAPIKeyRejectedOnRange(t *testing.T) {
	resp, err := doReqAs("not-a-provisioned-key", http.MethodGet, baseURL+"/range?start=a&end=z", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// B4
func TestProvisionedKeysAreIsolated(t *testing.T) {
	otherKey := provision(t)
	defer func() { deprovision(t, otherKey).Body.Close() }()

	k := testKey(t, "shared")
	defer del(t, k)

	resp, err := doReqAs(otherKey, http.MethodPut, baseURL+"/key/"+k, strings.NewReader("visible-to-other-tenant-only"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// same key path, different tenant: must not see the other tenant's value.
	resp = mustReq(t, http.MethodGet, baseURL+"/key/"+k, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (tenants must not share data)", resp.StatusCode)
	}
}

// B5
func TestDeprovisionRevokesKey(t *testing.T) {
	otherKey := provision(t)

	resp := deprovision(t, otherKey)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /keys/{key} status = %d, want 204", resp.StatusCode)
	}

	resp2, err := doReqAs(otherKey, http.MethodGet, baseURL+"/key/"+testKey(t, "x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after deprovision = %d, want 401", resp2.StatusCode)
	}
}

// B6
func TestDeprovisionUnknownKey(t *testing.T) {
	resp := deprovision(t, "not-a-provisioned-key")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// B7
func TestPauseUnknownKey(t *testing.T) {
	resp := pause(t, "not-a-provisioned-key")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// B8
func TestResumeUnknownKey(t *testing.T) {
	resp := resume(t, "not-a-provisioned-key")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// B9
func TestPausedTenantRejectsRequests(t *testing.T) {
	otherKey := provision(t)
	defer func() { deprovision(t, otherKey).Body.Close() }()

	resp := pause(t, otherKey)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST pause status = %d, want 204", resp.StatusCode)
	}

	resp2, err := doReqAs(otherKey, http.MethodGet, baseURL+"/key/"+testKey(t, "x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status while paused = %d, want 503", resp2.StatusCode)
	}

	resume(t, otherKey).Body.Close()
}

// B10
func TestPauseThenResumePreservesData(t *testing.T) {
	otherKey := provision(t)
	defer func() { deprovision(t, otherKey).Body.Close() }()

	k := "durable"
	resp, err := doReqAs(otherKey, http.MethodPut, baseURL+"/key/"+k, strings.NewReader("still-here"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	pause(t, otherKey).Body.Close()
	resume(t, otherKey).Body.Close()

	// The client's cached socket connection from before the pause is stale;
	// the very first request after resume can surface that as a single
	// failed round trip before the client reconnects. Retry once to ride
	// over that reconnect rather than asserting on which attempt succeeds.
	var got *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		got, err = doReqAs(otherKey, http.MethodGet, baseURL+"/key/"+k, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.StatusCode == http.StatusOK {
			break
		}
		got.Body.Close()
	}
	defer got.Body.Close()
	if got.StatusCode != http.StatusOK {
		t.Fatalf("GET after resume status = %d, want 200", got.StatusCode)
	}
	body := new(bytes.Buffer)
	body.ReadFrom(got.Body)
	if body.String() != "still-here" {
		t.Fatalf("GET body after resume = %q, want %q", body.String(), "still-here")
	}
}

// B11
func TestAdminCORSHeaders(t *testing.T) {
	resp, err := http.Post(adminURL+"/keys", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}

	req, err := http.NewRequest(http.MethodOptions, adminURL+"/keys", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", resp2.StatusCode)
	}
}
