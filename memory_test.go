package easylog

import (
	"bytes"
	"errors"
	"slices"
	"testing"
)

func retainedMemoryJSON(test *testing.T, consumer MemoryConsumer) [][]byte {
	test.Helper()
	store, ok := consumer.(*memoryStore)
	if !ok {
		test.Fatal("memory retention is not enabled")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return slices.Clone(store.records)
}

func TestMemoryStoreEvictsOldestPrefix(t *testing.T) {
	store := newMemoryStore(5)
	store.append([]byte("aa"))
	store.append([]byte("bb"))
	store.append([]byte("ccc"))
	records := store.records
	if len(records) != 2 || string(records[0]) != "bb" || string(records[1]) != "ccc" {
		t.Fatalf("unexpected FIFO result: %+v", records)
	}
	if store.Len() != 2 || store.Bytes() != 5 {
		t.Fatalf("retained store: records=%d bytes=%d", store.Len(), store.Bytes())
	}
}

func TestMemoryStoreRejectsOversizedRecordWithoutEviction(t *testing.T) {
	store := newMemoryStore(4)
	store.append([]byte("ok"))
	store.append([]byte("large"))
	records := store.records
	if len(records) != 1 || string(records[0]) != "ok" {
		t.Fatalf("existing record was changed: %+v", records)
	}
	if store.Len() != 1 || store.Bytes() != 2 {
		t.Fatalf("retained store: records=%d bytes=%d", store.Len(), store.Bytes())
	}
}

func TestMemoryStoreRetainsJSONWithoutCopying(test *testing.T) {
	store := newMemoryStore(100)
	jsonContent := []byte(`{"msg":"event"}`)
	store.append(jsonContent)
	if store.Len() != 1 || store.Bytes() != int64(len(jsonContent)) {
		test.Fatalf("retained store: records=%d bytes=%d", store.Len(), store.Bytes())
	}
	if string(store.records[0]) != `{"msg":"event"}` {
		test.Fatalf("retained JSON = %q", store.records[0])
	}
	if &store.records[0][0] != &jsonContent[0] {
		test.Fatal("memory copied the owned JSON bytes")
	}
}

func TestMemoryStoreStartsEmpty(test *testing.T) {
	store := newMemoryStore(100)
	if store.Len() != 0 || store.Bytes() != 0 {
		test.Fatalf("empty store: records=%d bytes=%d", store.Len(), store.Bytes())
	}
}

func TestMemoryStoreTakeReturnsImmediatelyWhenEmpty(t *testing.T) {
	store := newMemoryStore(100)
	records, err := store.Take(10)
	if err != nil {
		t.Fatal(err)
	}
	if records != nil || store.Len() != 0 || store.Bytes() != 0 {
		t.Fatalf("empty take: records=%v length=%d bytes=%d", records, store.Len(), store.Bytes())
	}
}

func TestMemoryStoreTakeHonorsPageSize(t *testing.T) {
	store := newMemoryStore(100)
	store.append([]byte(`{"msg":"first"}`))
	store.append([]byte(`{"msg":"second"}`))
	remaining := []byte(`{"msg":"third"}`)
	store.append(remaining)

	records, err := store.Take(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || string(records[0]) != `{"msg":"first"}` || string(records[1]) != `{"msg":"second"}` {
		t.Fatalf("unexpected result: %+v", records)
	}
	if store.Len() != 1 || store.Bytes() != int64(len(remaining)) {
		t.Fatalf("remaining store: records=%d bytes=%d", store.Len(), store.Bytes())
	}
	records, err = store.Take(10)
	if err != nil || len(records) != 1 || !bytes.Equal(records[0], remaining) {
		t.Fatalf("oversized page: records=%q err=%v", records, err)
	}
	if store.Len() != 0 || store.Bytes() != 0 || store.records != nil {
		t.Fatalf("drained store: records=%d bytes=%d", store.Len(), store.Bytes())
	}
}

func TestMemoryStoreRejectsNegativePageSize(t *testing.T) {
	store := newMemoryStore(100)
	content := []byte(`{"msg":"retained"}`)
	store.append(content)
	records, err := store.Take(-1)
	if records != nil || !errors.Is(err, ErrInvalidPageSize) {
		t.Fatalf("records=%q err=%v, want nil and %v", records, err, ErrInvalidPageSize)
	}
	if store.Len() != 1 || store.Bytes() != int64(len(content)) || !bytes.Equal(store.records[0], content) {
		t.Fatal("invalid page size changed retained data")
	}
}

func TestMemoryStoreTakeAllReturnsIndependentJSON(test *testing.T) {
	store := newMemoryStore(100)
	content := []byte(`{"msg":"event"}`)
	original := bytes.Clone(content)
	store.append(content)
	store.append(content)
	retainedSlots := store.records
	records, err := store.Take(0)
	if err != nil || len(records) != 2 {
		test.Fatalf("drain: records=%d err=%v, want two records", len(records), err)
	}
	if !bytes.Equal(records[0], original) || !bytes.Equal(records[1], original) {
		test.Fatalf("returned JSON = %q, want %q", records, original)
	}
	if store.Len() != 0 || store.Bytes() != 0 || store.records != nil {
		test.Fatal("Take(0) did not fully drain the store")
	}
	for _, retained := range retainedSlots {
		if retained != nil {
			test.Fatal("drained queue slot still retains JSON storage")
		}
	}
	records[0][0] = '!'
	if !bytes.Equal(content, original) || !bytes.Equal(records[1], original) {
		test.Fatal("returned JSON shares storage with outputs or another result")
	}
	records[0] = nil
	store.append([]byte(`{"msg":"next"}`))
	if !bytes.Equal(records[1], original) || store.Len() != 1 {
		test.Fatal("reusing the queue changed a returned record")
	}
}

func TestMemoryStoreTakeThenAppendPreservesEviction(test *testing.T) {
	first := []byte(`{"id":1}`)
	store := newMemoryStore(2 * int64(len(first)))
	store.append(first)
	store.append([]byte(`{"id":2}`))
	taken, err := store.Take(1)
	if err != nil || len(taken) != 1 || !bytes.Equal(taken[0], first) {
		test.Fatalf("first page: records=%q err=%v", taken, err)
	}
	store.append([]byte(`{"id":3}`))
	store.append([]byte(`{"id":4}`))
	if store.Len() != 2 || store.Bytes() != 2*int64(len(first)) {
		test.Fatalf("retained after eviction: records=%d bytes=%d", store.Len(), store.Bytes())
	}
	records, err := store.Take(0)
	if err != nil || len(records) != 2 || string(records[0]) != `{"id":3}` || string(records[1]) != `{"id":4}` {
		test.Fatalf("remaining FIFO: records=%q err=%v", records, err)
	}
	if !bytes.Equal(taken[0], first) || store.Len() != 0 || store.Bytes() != 0 {
		test.Fatal("eviction changed the first page or drain accounting")
	}
}
