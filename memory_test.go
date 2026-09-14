package easylog

import (
	"errors"
	"testing"

	"github.com/LazyEasyDev/EasyLog/internal/core"
)

func TestMemoryStoreEvictsOldestPrefix(t *testing.T) {
	store := newMemoryStore(5)
	store.append(core.NewRecord(1, 0, []byte("aa")))
	store.append(core.NewRecord(2, 0, []byte("bb")))
	store.append(core.NewRecord(3, 0, []byte("ccc")))
	records, err := store.Take(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Sequence() != 2 || records[1].Sequence() != 3 {
		t.Fatalf("unexpected FIFO result: %+v", records)
	}
}

func TestMemoryStoreRejectsOversizedRecordWithoutEviction(t *testing.T) {
	store := newMemoryStore(4)
	store.append(core.NewRecord(1, 0, []byte("ok")))
	store.append(core.NewRecord(2, 0, []byte("large")))
	records, err := store.Take(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Sequence() != 1 {
		t.Fatalf("existing record was changed: %+v", records)
	}
}

func TestMemoryStoreTakeReturnsImmediatelyWhenEmpty(t *testing.T) {
	store := newMemoryStore(100)
	records, err := store.Take(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records, want none", len(records))
	}
}

func TestMemoryStoreTakeHonorsPageSize(t *testing.T) {
	store := newMemoryStore(100)
	store.append(core.NewRecord(1, 0, []byte("a")))
	store.append(core.NewRecord(2, 0, []byte("b")))
	store.append(core.NewRecord(3, 0, []byte("c")))

	records, err := store.Take(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Sequence() != 1 || records[1].Sequence() != 2 {
		t.Fatalf("unexpected result: %+v", records)
	}
	if store.Len() != 1 || store.Bytes() != 1 {
		t.Fatalf("remaining store: records=%d bytes=%d", store.Len(), store.Bytes())
	}
}

func TestMemoryStoreRejectsNegativePageSize(t *testing.T) {
	store := newMemoryStore(100)
	_, err := store.Take(-1)
	if !errors.Is(err, ErrInvalidPageSize) {
		t.Fatalf("got %v, want %v", err, ErrInvalidPageSize)
	}
}
