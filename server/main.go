package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
)

// note this cant be greater than 4gb for now
const MaxValueBytes = 50 * 1024 * 1024 // 50MB

const MaxStorageBytes = 1024 * 1024 * 1024 // 1GB per tenant

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func WithCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Api-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func withLogging(h http.Handler, verbose bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusWriter{ResponseWriter: w, code: 200}
		h.ServeHTTP(rw, r)
		if verbose {
			log.Printf("%s %s -> %d", r.Method, r.URL, rw.code)
		}
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrUint64(key string, fallback uint64) uint64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func main() {
	verbose := flag.Bool("verbose", os.Getenv("FREDB_VERBOSE") == "1", "log requests")
	dataRoot := flag.String("data-root", envOr("FREDB_DATA_ROOT", "/tmp/fredb-data"), "root dir for per-tenant data")
	sockRoot := flag.String("sock-root", envOr("FREDB_SOCK_ROOT", "/tmp/fredb-socks"), "root dir for per-tenant engine sockets")
	apiAddr := flag.String("api-addr", envOr("FREDB_API_ADDR", ":8080"), "api port address")
	adminAddr := flag.String("admin-addr", envOr("FREDB_ADMIN_ADDR", ":8081"), "admin port address")
	maxStorageBytes := flag.Uint64("max-storage-bytes", envOrUint64("FREDB_MAX_STORAGE_BYTES", MaxStorageBytes), "per-tenant storage cap in bytes")
	flag.Parse()

	manager := NewDatabaseManager(*dataRoot, *sockRoot, MaxValueBytes, *maxStorageBytes)
	if err := manager.LoadAll(); err != nil {
		log.Fatal(err)
	}

	admin := NewAdminHandler(manager)
	api := NewAPIHandler(manager)

	go func() {
		log.Printf("admin at http://localhost%s", *adminAddr)
		log.Fatal(http.ListenAndServe(*adminAddr, withLogging(admin, *verbose)))
	}()

	log.Printf("api at http://localhost%s", *apiAddr)
	log.Fatal(http.ListenAndServe(*apiAddr, withLogging(api, *verbose)))
}
