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
	head     int
	count    int
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

	for s.count > 0 && s.bytes+size > s.maxBytes {
		oldestSize := int64(len(s.records[s.head]))
		s.records[s.head] = nil
		s.head++
		if s.head == len(s.records) {
			s.head = 0
		}
		s.count--
		s.bytes -= oldestSize
	}

	if s.count == len(s.records) {
		records := make([][]byte, max(8, 2*len(s.records)))
		copied := copy(records, s.records[s.head:])
		copy(records[copied:], s.records[:s.head])
		s.records = records
		s.head = 0
	}
	tail := s.head + s.count
	if tail >= len(s.records) {
		tail -= len(s.records)
	}
	s.records[tail] = jsonContent
	s.count++
	s.bytes += size
}

func (s *memoryStore) Take(pageSize int) ([][]byte, error) {
	if pageSize < 0 {
		return nil, ErrInvalidPageSize
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.count == 0 {
		return nil, nil
	}

	takeCount := s.count
	if pageSize > 0 && pageSize < takeCount {
		takeCount = pageSize
	}

	result := make([][]byte, takeCount)
	for index := range result {
		jsonContent := s.records[s.head]
		result[index] = bytes.Clone(jsonContent)
		s.records[s.head] = nil
		s.head++
		if s.head == len(s.records) {
			s.head = 0
		}
		s.bytes -= int64(len(jsonContent))
	}

	s.count -= takeCount
	if s.count == 0 {
		s.records = nil
		s.head = 0
	}
	return result, nil
}

func (s *memoryStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *memoryStore) Bytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytes
}
