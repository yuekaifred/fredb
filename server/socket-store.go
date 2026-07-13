package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type SocketStore struct {
	mu   sync.Mutex
	conn net.Conn
	r    *bufio.Reader
}

func (s *SocketStore) ensureConnected(path string) error {
	if s.conn != nil {
		return nil
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("engine offline: %w", err)
	}
	s.conn = conn
	s.r = bufio.NewReader(conn)
	return nil
}

func (s *SocketStore) reconnect(path string) error {
	if s.conn != nil {
		s.conn.Close()
	}
	s.conn = nil
	s.r = nil
	return s.ensureConnected(path)
}

func encodeKey(k string) string { return url.PathEscape(k) }
func decodeKey(k string) string { s, _ := url.PathUnescape(k); return s }

func (s *SocketStore) Get(path, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureConnected(path); err != nil {
		return "", false, err
	}
	if _, err := fmt.Fprintln(s.conn, "GET "+encodeKey(key)); err != nil {
		s.reconnect(path)
		return "", false, err
	}
	first, err := s.r.ReadString('\n')
	if err != nil {
		s.reconnect(path)
		return "", false, err
	}
	first = strings.TrimRight(first, "\r\n")
	if first == "NOT_FOUND" {
		return "", false, nil
	}
	n, err := strconv.Atoi(first)
	if err != nil {
		return "", false, fmt.Errorf("bad response: %s", first)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(s.r, buf); err != nil {
		s.reconnect(path)
		return "", false, err
	}
	return string(buf), true, nil
}

func (s *SocketStore) Put(path, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureConnected(path); err != nil {
		return err
	}

	header := fmt.Sprintf("PUT %s %d\n", encodeKey(key), len(value))
	if _, err := fmt.Fprint(s.conn, header+value); err != nil {
		s.reconnect(path)
		return err
	}
	reply, err := s.r.ReadString('\n')
	if err != nil {
		s.reconnect(path)
		return err
	}
	reply = strings.TrimRight(reply, "\r\n")
	if strings.HasPrefix(reply, "ERR") {
		return fmt.Errorf("%s", reply)
	}
	return nil
}

func (s *SocketStore) Delete(path, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureConnected(path); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(s.conn, "DEL "+encodeKey(key)); err != nil {
		s.reconnect(path)
		return err
	}
	_, err := s.r.ReadString('\n')
	if err != nil {
		s.reconnect(path)
	}
	return err
}

func (s *SocketStore) TotalSize(path string) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureConnected(path); err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintln(s.conn, "SIZE"); err != nil {
		s.reconnect(path)
		return 0, err
	}
	line, err := s.r.ReadString('\n')
	if err != nil {
		s.reconnect(path)
		return 0, err
	}
	line = strings.TrimRight(line, "\r\n")
	n, err := strconv.ParseUint(line, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad response: %s", line)
	}
	return n, nil
}

func (s *SocketStore) Range(path, start, end string) ([]KV, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureConnected(path); err != nil {
		return nil, err
	}

	if _, err := fmt.Fprintln(s.conn, "RANGE "+encodeKey(start)+" "+encodeKey(end)); err != nil {
		s.reconnect(path)
		return nil, err
	}

	countLine, err := s.r.ReadString('\n')
	if err != nil {
		s.reconnect(path)
		return nil, err
	}
	countLine = strings.TrimRight(countLine, "\r\n")
	if !strings.HasPrefix(countLine, "COUNT ") {
		return nil, fmt.Errorf("unexpected: %s", countLine)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(countLine, "COUNT "))
	if err != nil {
		return nil, err
	}

	pairs := make([]KV, 0, n)
	for i := 0; i < n; i++ {
		key, err := s.r.ReadString('\n')
		if err != nil {
			s.reconnect(path)
			return nil, fmt.Errorf("range read key: %w", err)
		}
		key = decodeKey(strings.TrimRight(key, "\r\n"))
		vlenStr, err := s.r.ReadString('\n')
		if err != nil {
			s.reconnect(path)
			return nil, fmt.Errorf("range read vlen: %w", err)
		}
		vlen, err := strconv.Atoi(strings.TrimRight(vlenStr, "\r\n"))
		if err != nil {
			return nil, fmt.Errorf("range bad vlen: %w", err)
		}
		val := make([]byte, vlen)
		if _, err := io.ReadFull(s.r, val); err != nil {
			s.reconnect(path)
			return nil, fmt.Errorf("range read val: %w", err)
		}
		pairs = append(pairs, KV{Key: key, Value: string(val)})
	}
	endLine, err := s.r.ReadString('\n')
	if err != nil {
		s.reconnect(path)
		return nil, fmt.Errorf("range read end (expected END): %w", err)
	}
	if strings.TrimRight(endLine, "\r\n") != "END" {
		s.reconnect(path)
		return nil, fmt.Errorf("range expected END, got: %s", endLine)
	}
	return pairs, nil
}
