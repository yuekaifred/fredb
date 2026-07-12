package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func NewAPIHandler(man *DatabaseManager) http.Handler {
	api := http.NewServeMux()

	api.HandleFunc("/key/", func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-Api-Key")
		key := strings.TrimPrefix(r.URL.Path, "/key/")
		switch r.Method {
		case http.MethodGet:
			val, ok, err := man.Get(apiKey, key)
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
			body, err := io.ReadAll(io.LimitReader(r.Body, int64(MaxValueBytes)+1))
			if err != nil {
				http.Error(w, "failed to read body", http.StatusBadRequest)
				return
			}
			if err := man.Put(apiKey, key, string(body)); err != nil {
				http.Error(w, err.Message, err.Status)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case http.MethodDelete:
			if err := man.Delete(apiKey, key); err != nil {
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
		pairs, err := man.Range(apiKey, start, end)
		if err != nil {
			http.Error(w, err.Message, err.Status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pairs)
	})

	return WithCORS(api)
}
