package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func NewPublicHandler(man *DatabaseManager) http.Handler {
	api := http.NewServeMux()

	api.HandleFunc("/provision", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			apiKey, err := man.RequestProvision()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"api_key": apiKey})

		case http.MethodDelete:
			if err := man.Deprovision(r.Header.Get("X-Api-Key")); err != nil {
				http.Error(w, err.Message, err.Status)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	api.HandleFunc("/key/", func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-Api-Key")
		key := strings.TrimPrefix(r.URL.Path, "/key/")
		switch r.Method {
		case http.MethodGet:
			db, err := man.Lookup(apiKey)
			if err != nil {
				http.Error(w, err.Message, err.Status)
				return
			}
			val, ok, err := db.Get(key)
			if err != nil {
				http.Error(w, err.Message, err.Status)
				return
			}
			if !ok {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, val)

		case http.MethodPut:
			// we handle size limits db side
			body, readErr := io.ReadAll(io.LimitReader(r.Body, int64(MaxValueBytes)+1))
			if readErr != nil {
				http.Error(w, "failed to read body", http.StatusBadRequest)
				return
			}
			db, err := man.Lookup(apiKey)
			if err != nil {
				http.Error(w, err.Message, err.Status)
				return
			}
			if err := db.Put(key, string(body)); err != nil {
				http.Error(w, err.Message, err.Status)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case http.MethodDelete:
			db, err := man.Lookup(apiKey)
			if err != nil {
				http.Error(w, err.Message, err.Status)
				return
			}
			if err := db.Delete(key); err != nil {
				http.Error(w, err.Message, err.Status)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	api.HandleFunc("/range", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		apiKey := r.Header.Get("X-Api-Key")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")
		db, err := man.Lookup(apiKey)
		if err != nil {
			http.Error(w, err.Message, err.Status)
			return
		}
		pairs, err := db.Range(start, end)
		if err != nil {
			http.Error(w, err.Message, err.Status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pairs)
	})

	return WithCORS(api)
}
