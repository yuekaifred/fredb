package main

import (
	"fmt"
	"sync"
	"testing"
)

func TestCgoEngineBasic(t *testing.T) {
	e, err := OpenCgoEngine(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	if _, ok, err := e.Get("missing"); err != nil || ok {
		t.Fatalf("get missing: ok=%v err=%v", ok, err)
	}

	if err := e.Put("k1", "v1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	val, ok, err := e.Get("k1")
	if err != nil || !ok || val != "v1" {
		t.Fatalf("get k1: val=%q ok=%v err=%v", val, ok, err)
	}

	size, err := e.TotalSize()
	if err != nil {
		t.Fatalf("total size: %v", err)
	}
	if size == 0 {
		t.Fatalf("expected nonzero size after put")
	}

	if err := e.Delete("k1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, err := e.Get("k1"); err != nil || ok {
		t.Fatalf("get after delete: ok=%v err=%v", ok, err)
	}
}

func TestCgoEngineRange(t *testing.T) {
	e, err := OpenCgoEngine(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	want := map[string]string{"a": "1", "b": "2", "c": "3", "z": "skip"}
	for k, v := range want {
		if err := e.Put(k, v); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}

	pairs, err := e.Range("a", "d")
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	got := map[string]string{}
	for _, kv := range pairs {
		got[kv.Key] = kv.Value
	}
	for _, k := range []string{"a", "b", "c"} {
		if got[k] != want[k] {
			t.Fatalf("range missing/wrong %s: got %q want %q", k, got[k], want[k])
		}
	}
	if _, ok := got["z"]; ok {
		t.Fatalf("range should not include z, out of bounds")
	}
}

func TestCgoEngineConcurrentPuts(t *testing.T) {
	e, err := OpenCgoEngine(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	const goroutines = 50
	const perGoroutine = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				key := fmt.Sprintf("g%d-k%d", g, i)
				if err := e.Put(key, key); err != nil {
					t.Errorf("put %s: %v", key, err)
				}
			}
		}(g)
	}
	wg.Wait()

	for g := 0; g < goroutines; g++ {
		for i := 0; i < perGoroutine; i++ {
			key := fmt.Sprintf("g%d-k%d", g, i)
			val, ok, err := e.Get(key)
			if err != nil || !ok || val != key {
				t.Fatalf("get %s: val=%q ok=%v err=%v", key, val, ok, err)
			}
		}
	}
}
