package easylog

import (
	"bytes"
	"errors"
	"sync"
)

// ErrInvalidPageSize indicates a negative Take page size.
var ErrInvalidPageSize = errors.New("easylog: invalid page size")

// MemoryConsumer reads retained JSON records in FIFO order and reports queue state.
type MemoryConsumer interface {
	// Take removes up to pageSize oldest records as caller-owned JSON slices, each ending in one newline.
	// Zero drains the queue; negative returns ErrInvalidPageSize; empty returns nil, nil.
	Take(pageSize int) ([][]byte, error)
	Len() int
	Bytes() int64
}

type memoryStore struct {
	mu       sync.Mutex
	records  [][]byte
	bytes    int64
	maxBytes int64
}

func newMemoryStore(maxBytes int64) *memoryStore {
	return &memoryStore{maxBytes: maxBytes}
}

func (s *memoryStore) append(jsonContent []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	size := int64(len(jsonContent))
	if size > s.maxBytes {
		return
	}

	for len(s.records) > 0 && s.bytes+size > s.maxBytes {
		oldestSize := int64(len(s.records[0]))
		s.records[0] = nil
		s.records = s.records[1:]
		s.bytes -= oldestSize
	}

	s.records = append(s.records, jsonContent)
	s.bytes += size
}

func (s *memoryStore) Take(pageSize int) ([][]byte, error) {
	if pageSize < 0 {
		return nil, ErrInvalidPageSize
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.records) == 0 {
		return nil, nil
	}

	takeCount := len(s.records)
	if pageSize > 0 && pageSize < takeCount {
		takeCount = pageSize
	}

	result := make([][]byte, takeCount)
	var takeBytes int64
	for index, jsonContent := range s.records[:takeCount] {
		result[index] = bytes.Clone(jsonContent)
		takeBytes += int64(len(jsonContent))
	}

	clear(s.records[:takeCount])
	s.records = s.records[takeCount:]
	if len(s.records) == 0 {
		s.records = nil
	}
	s.bytes -= takeBytes
	return result, nil
}

func (s *memoryStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

func (s *memoryStore) Bytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytes
}
