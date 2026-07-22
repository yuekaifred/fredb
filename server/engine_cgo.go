package main

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -L${SRCDIR}/../engine/build -lfredb_core -lstdc++ -pthread -lm
#include "../engine/fredb_c.h"
#include <stdlib.h>
*/
import "C"
import (
	"errors"
	"unsafe"
)

type CgoEngine struct {
	h C.fredb_handle
}

func lastError() error {
	return errors.New(C.GoString(C.fredb_last_error()))
}

func OpenCgoEngine(dir string) (*CgoEngine, error) {
	cdir := C.CString(dir)
	defer C.free(unsafe.Pointer(cdir))

	h := C.fredb_open(cdir, C.size_t(len(dir)))
	if h == nil {
		return nil, lastError()
	}
	return &CgoEngine{h: h}, nil
}

func (e *CgoEngine) Close() {
	C.fredb_close(e.h)
	e.h = nil
}

func (e *CgoEngine) Get(key string) (string, bool, error) {
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))

	var out *C.char
	var outLen C.size_t
	rc := C.fredb_get(e.h, ckey, C.size_t(len(key)), &out, &outLen)
	if rc < 0 {
		return "", false, lastError()
	}
	if rc == 0 {
		return "", false, nil
	}
	defer C.fredb_free_buf(out)
	return C.GoStringN(out, C.int(outLen)), true, nil
}

func (e *CgoEngine) Put(key, value string) error {
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))
	cval := C.CString(value)
	defer C.free(unsafe.Pointer(cval))

	if rc := C.fredb_put(e.h, ckey, C.size_t(len(key)), cval, C.size_t(len(value))); rc != 0 {
		return lastError()
	}
	return nil
}

func (e *CgoEngine) Delete(key string) error {
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))

	if rc := C.fredb_remove(e.h, ckey, C.size_t(len(key))); rc != 0 {
		return lastError()
	}
	return nil
}

func (e *CgoEngine) TotalSize() (uint64, error) {
	size := C.fredb_total_size(e.h)
	if size < 0 {
		return 0, lastError()
	}
	return uint64(size), nil
}

func (e *CgoEngine) Range(start, end string) ([]KV, error) {
	cstart := C.CString(start)
	defer C.free(unsafe.Pointer(cstart))
	cend := C.CString(end)
	defer C.free(unsafe.Pointer(cend))

	var out *C.char
	var outLen C.size_t
	if rc := C.fredb_get_range(e.h, cstart, C.size_t(len(start)), cend, C.size_t(len(end)), &out, &outLen); rc != 0 {
		return nil, lastError()
	}
	defer C.fredb_free_buf(out)

	buf := unsafe.Slice((*byte)(unsafe.Pointer(out)), int(outLen))
	return decodeRangeBuf(buf)
}

func decodeRangeBuf(buf []byte) ([]KV, error) {
	if len(buf) < 4 {
		return nil, errors.New("range buffer too short")
	}
	count := le32(buf)
	buf = buf[4:]

	pairs := make([]KV, 0, count)
	for range count {
		if len(buf) < 4 {
			return nil, errors.New("range buffer truncated (key length)")
		}
		klen := le32(buf)
		buf = buf[4:]
		if uint32(len(buf)) < klen {
			return nil, errors.New("range buffer truncated (key)")
		}
		key := string(buf[:klen])
		buf = buf[klen:]

		if len(buf) < 4 {
			return nil, errors.New("range buffer truncated (value length)")
		}
		vlen := le32(buf)
		buf = buf[4:]
		if uint32(len(buf)) < vlen {
			return nil, errors.New("range buffer truncated (value)")
		}
		value := string(buf[:vlen])
		buf = buf[vlen:]

		pairs = append(pairs, KV{Key: key, Value: value})
	}
	return pairs, nil
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
