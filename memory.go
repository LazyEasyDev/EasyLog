package easylog

import (
	"errors"
	"sync"
)

var (
	// ErrInvalidPageSize indicates a negative Take page size.
	ErrInvalidPageSize = errors.New("easylog: invalid page size")
)

// MemoryConsumer destructively reads records retained in the memory FIFO.
type MemoryConsumer interface {
	// Take immediately removes up to pageSize oldest records. Zero returns all available records.
	Take(pageSize int) ([]Record, error)
	Len() int
	Bytes() int64
}

type memoryStore struct {
	mu       sync.Mutex
	records  []Record
	bytes    int64
	maxBytes int64
}

func newMemoryStore(maxBytes int64) *memoryStore {
	return &memoryStore{maxBytes: maxBytes}
}

func (s *memoryStore) append(record Record) {
	s.mu.Lock()
	defer s.mu.Unlock()

	size := record.Size()
	if size > s.maxBytes {
		return
	}

	for len(s.records) > 0 && s.bytes+size > s.maxBytes {
		oldest := s.records[0]
		oldestSize := oldest.Size()
		s.records[0] = Record{}
		s.records = s.records[1:]
		s.bytes -= oldestSize
	}

	s.records = append(s.records, record)
	s.bytes += size
}

func (s *memoryStore) Take(pageSize int) ([]Record, error) {
	if pageSize < 0 {
		return nil, ErrInvalidPageSize
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.records) == 0 {
		return nil, nil
	}
	return s.takeLocked(pageSize), nil
}

func (s *memoryStore) takeLocked(pageSize int) []Record {
	takeCount := len(s.records)
	if pageSize > 0 && pageSize < takeCount {
		takeCount = pageSize
	}

	var takeBytes int64
	for _, record := range s.records[:takeCount] {
		takeBytes += record.Size()
	}

	result := make([]Record, takeCount)
	copy(result, s.records[:takeCount])
	clear(s.records[:takeCount])
	s.records = s.records[takeCount:]
	if len(s.records) == 0 {
		s.records = nil
	}
	s.bytes -= takeBytes
	return result
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
