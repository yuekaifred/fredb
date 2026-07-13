package main

import "net/http"

type StoreError struct {
	Status  int
	Message string
}

func (e *StoreError) Error() string { return e.Message }

func errUnavailable(err error) *StoreError {
	return &StoreError{Status: http.StatusServiceUnavailable, Message: err.Error()}
}

var (
	errTooLarge         = &StoreError{Status: http.StatusRequestEntityTooLarge, Message: "value too large"}
	errStorageFull      = &StoreError{Status: http.StatusInsufficientStorage, Message: "storage limit exceeded"}
	errRateLimited      = &StoreError{Status: http.StatusTooManyRequests, Message: "rate limit exceeded"}
	errEmptyKey         = &StoreError{Status: http.StatusBadRequest, Message: "missing key"}
	errEmptyRangeBounds = &StoreError{Status: http.StatusBadRequest, Message: "start and end required"}
	ErrUnknownAPIKey    = &StoreError{Status: http.StatusUnauthorized, Message: "unknown api key"}
)

type Database struct {
	APIKey          string
	maxValueBytes   int
	maxStorageBytes uint64

	engine *EngineProcess
	store  *SocketStore
}

func NewDatabase(apiKey, sockPath, dataDirPath string, maxValueBytes int, maxStorageBytes uint64) (*Database, error) {
	engine := &EngineProcess{SockPath: sockPath, DataDirPath: dataDirPath}
	if err := engine.Spawn(); err != nil {
		return nil, err
	}
	return &Database{
		APIKey:          apiKey,
		maxValueBytes:   maxValueBytes,
		maxStorageBytes: maxStorageBytes,
		engine:          engine,
	}, nil
}

// dont create store until we actually use it!
func (db *Database) ensureStore() *SocketStore {
	if db.store == nil {
		db.store = &SocketStore{}
	}
	return db.store
}

func (db *Database) sockPath() string {
	return db.engine.SockPath
}

func (db *Database) DestroyCompletely() error {
	return db.engine.DestroyCompletely()
}

func (db *Database) Pause() error {
	return db.engine.Destroy()
}

func (db *Database) Resume() error {
	return db.engine.Spawn()
}

func (db *Database) Get(key string) (string, bool, *StoreError) {
	if key == "" {
		return "", false, errEmptyKey
	}
	val, ok, err := db.ensureStore().Get(db.sockPath(), key)
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
		return errTooLarge
	}
	size, err := db.ensureStore().TotalSize(db.sockPath())
	if err != nil {
		return errUnavailable(err)
	}
	if size >= db.maxStorageBytes {
		return errStorageFull
	}
	if err := db.ensureStore().Put(db.sockPath(), key, value); err != nil {
		return errUnavailable(err)
	}
	return nil
}

func (db *Database) Delete(key string) *StoreError {
	if key == "" {
		return errEmptyKey
	}
	if err := db.ensureStore().Delete(db.sockPath(), key); err != nil {
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
	pairs, err := db.ensureStore().Range(db.sockPath(), start, end)
	if err != nil {
		return nil, errUnavailable(err)
	}
	return pairs, nil
}
