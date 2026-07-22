package main

import (
	"net/http"
	"os"
)

type StoreError struct {
	Status  int
	Message string
}

func (e *StoreError) Error() string { return e.Message }

func errUnavailable(err error) *StoreError {
	return &StoreError{Status: http.StatusServiceUnavailable, Message: err.Error()}
}

var (
	ErrTooLarge         = &StoreError{Status: http.StatusRequestEntityTooLarge, Message: "value too large"}
	errStorageFull      = &StoreError{Status: http.StatusInsufficientStorage, Message: "storage limit exceeded"}
	ErrRateLimited      = &StoreError{Status: http.StatusTooManyRequests, Message: "rate limit exceeded"}
	errEmptyKey         = &StoreError{Status: http.StatusBadRequest, Message: "missing key"}
	errEmptyRangeBounds = &StoreError{Status: http.StatusBadRequest, Message: "start and end required"}
	ErrUnknownAPIKey    = &StoreError{Status: http.StatusUnauthorized, Message: "unknown api key"}
)

var errPaused = &StoreError{Status: http.StatusServiceUnavailable, Message: "database is paused"}

type Database struct {
	APIKey          string
	dataDirPath     string
	maxValueBytes   int
	maxStorageBytes uint64

	engine *CgoEngine
}

func NewDatabase(apiKey, dataDirPath string, maxValueBytes int, maxStorageBytes uint64) (*Database, error) {
	engine, err := OpenCgoEngine(dataDirPath)
	if err != nil {
		return nil, err
	}
	return &Database{
		APIKey:          apiKey,
		dataDirPath:     dataDirPath,
		maxValueBytes:   maxValueBytes,
		maxStorageBytes: maxStorageBytes,
		engine:          engine,
	}, nil
}

func (db *Database) DestroyCompletely() error {
	if db.engine != nil {
		db.engine.Close()
		db.engine = nil
	}
	return os.RemoveAll(db.dataDirPath)
}

func (db *Database) Pause() error {
	if db.engine == nil {
		return nil
	}
	db.engine.Close()
	db.engine = nil
	return nil
}

func (db *Database) Resume() error {
	if db.engine != nil {
		return nil
	}
	engine, err := OpenCgoEngine(db.dataDirPath)
	if err != nil {
		return err
	}
	db.engine = engine
	return nil
}

func (db *Database) Get(key string) (string, bool, *StoreError) {
	if key == "" {
		return "", false, errEmptyKey
	}
	if db.engine == nil {
		return "", false, errPaused
	}
	val, ok, err := db.engine.Get(key)
	if err != nil {
		return "", false, errUnavailable(err)
	}
	return val, ok, nil
}

func (db *Database) Put(key, value string) *StoreError {
	if key == "" {
		return errEmptyKey
	}
	if len(value) > db.maxValueBytes {
		return ErrTooLarge
	}
	if db.engine == nil {
		return errPaused
	}
	size, err := db.engine.TotalSize()
	if err != nil {
		return errUnavailable(err)
	}
	if size >= db.maxStorageBytes {
		return errStorageFull
	}
	if err := db.engine.Put(key, value); err != nil {
		return errUnavailable(err)
	}
	return nil
}

func (db *Database) Delete(key string) *StoreError {
	if key == "" {
		return errEmptyKey
	}
	if db.engine == nil {
		return errPaused
	}
	if err := db.engine.Delete(key); err != nil {
		return errUnavailable(err)
	}
	return nil
}

type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (db *Database) Range(start, end string) ([]KV, *StoreError) {
	if start == "" || end == "" {
		return nil, errEmptyRangeBounds
	}
	if db.engine == nil {
		return nil, errPaused
	}
	pairs, err := db.engine.Range(start, end)
	if err != nil {
		return nil, errUnavailable(err)
	}
	return pairs, nil
}
