package local_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/open-tempest-labs/zeaback/pkg/store"
	"github.com/open-tempest-labs/zeaback/pkg/store/local"
)

func TestLocalRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := local.New(t.TempDir())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	want := []byte("hello zeasnap payload")
	if err := s.Put(ctx, "data/pack-01.pack", bytes.NewReader(want)); err != nil {
		t.Fatalf("put: %v", err)
	}

	ok, err := s.Exists(ctx, "data/pack-01.pack")
	if err != nil || !ok {
		t.Fatalf("exists = %v, %v; want true, nil", ok, err)
	}

	rc, err := s.Get(ctx, "data/pack-01.pack")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, want) {
		t.Fatalf("get = %q; want %q", got, want)
	}
}

func TestLocalGetRange(t *testing.T) {
	ctx := context.Background()
	s, _ := local.New(t.TempDir())
	defer s.Close()

	if err := s.Put(ctx, "a/b.bin", bytes.NewReader([]byte("0123456789"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := s.GetRange(ctx, "a/b.bin", 3, 4)
	if err != nil {
		t.Fatalf("getrange: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "3456" {
		t.Fatalf("getrange = %q; want %q", got, "3456")
	}
}

func TestLocalListAndDelete(t *testing.T) {
	ctx := context.Background()
	s, _ := local.New(t.TempDir())
	defer s.Close()

	for _, k := range []string{"index/chunks.parquet", "index/packs.parquet", "data/p1.pack"} {
		if err := s.Put(ctx, k, bytes.NewReader([]byte("x"))); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}
	keys, err := s.List(ctx, "index")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("list index = %v; want 2 keys", keys)
	}

	if err := s.Delete(ctx, "data/p1.pack"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ok, _ := s.Exists(ctx, "data/p1.pack"); ok {
		t.Fatalf("deleted key still exists")
	}
	// Deleting a missing key is a no-op.
	if err := s.Delete(ctx, "data/missing.pack"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestLocalGetNotFound(t *testing.T) {
	ctx := context.Background()
	s, _ := local.New(t.TempDir())
	defer s.Close()

	_, err := s.Get(ctx, "nope")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get missing = %v; want ErrNotFound", err)
	}
}
