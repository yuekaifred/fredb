package main

import (
	"encoding/json"
	"net/http"
)

type RateLimitOverride struct {
	CapacityBytes     *float64 `json:"capacity_bytes,omitempty"`
	RefillBytesPerSec *float64 `json:"refill_bytes_per_sec,omitempty"`
	FlatOverheadBytes *float64 `json:"flat_overhead_bytes,omitempty"`
	WriteByteWeight   *float64 `json:"write_byte_weight,omitempty"`
	ReadByteWeight    *float64 `json:"read_byte_weight,omitempty"`
}

func applyRateLimitOverride(base RateLimitConfig, o RateLimitOverride) RateLimitConfig {
	cfg := base
	if o.CapacityBytes != nil {
		cfg.CapacityBytes = *o.CapacityBytes
	}
	if o.RefillBytesPerSec != nil {
		cfg.RefillBytesPerSec = *o.RefillBytesPerSec
	}
	if o.FlatOverheadBytes != nil {
		cfg.FlatOverheadBytes = *o.FlatOverheadBytes
	}
	if o.WriteByteWeight != nil {
		cfg.WriteByteWeight = *o.WriteByteWeight
	}
	if o.ReadByteWeight != nil {
		cfg.ReadByteWeight = *o.ReadByteWeight
	}
	return cfg
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeStoreErr(w http.ResponseWriter, err *StoreError) {
	writeJSON(w, err.Status, map[string]string{"error": err.Message})
}

func requireAdminKey(adminKey string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminKey == "" || r.Header.Get("X-Admin-Key") != adminKey {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid X-Admin-Key"})
			return
		}
		h(w, r)
	}
}

func NewAdminHandler(man *DatabaseManager, adminKey string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /admin/{key}/rate-limit", requireAdminKey(adminKey, func(w http.ResponseWriter, r *http.Request) {
		var override RateLimitOverride
		if err := json.NewDecoder(r.Body).Decode(&override); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		cfg := applyRateLimitOverride(man.rateLimit, override)
		db, err := man.LookupProvisioned(r.PathValue("key"))
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		db.SetRateLimitConfig(cfg)
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("GET /admin/storage", requireAdminKey(adminKey, func(w http.ResponseWriter, r *http.Request) {
		stats, err := man.StorageStats()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tenants_used_bytes":  stats.TenantsUsedBytes,
			"machine_total_bytes": stats.MachineTotalBytes,
			"machine_free_bytes":  stats.MachineFreeBytes,
		})
	}))

	return mux
}
