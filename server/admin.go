package main

import (
	"encoding/json"
	"net/http"
)

func NewAdminHandler(manager *DatabaseManager) http.Handler {
	admin := http.NewServeMux()

	admin.HandleFunc("POST /keys", func(w http.ResponseWriter, r *http.Request) {
		apiKey, err := manager.Provision()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"api_key": apiKey})
	})

	admin.HandleFunc("DELETE /keys/{key}", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.Deprovision(r.PathValue("key")); err != nil {
			http.Error(w, err.Message, err.Status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	admin.HandleFunc("POST /keys/{key}/pause", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.Pause(r.PathValue("key")); err != nil {
			http.Error(w, err.Message, err.Status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	admin.HandleFunc("POST /keys/{key}/resume", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.Resume(r.PathValue("key")); err != nil {
			http.Error(w, err.Message, err.Status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	return WithCORS(admin)
}
