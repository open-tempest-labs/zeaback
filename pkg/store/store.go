// Package store defines the immutable object-store abstraction that zeaback
// writes its repository to.
//
// zeaback only ever writes content-addressed, write-once objects (pack files and
// Parquet manifests). Because objects are never modified in place, any backend
// that can put/get/list/delete blobs is sufficient — including a plain local
// directory (which may itself be a mounted volumez FUSE path) or an adapter over
// a volumez storage backend.
package store

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound is returned by a Store when a key does not exist.
var ErrNotFound = errors.New("store: object not found")

// Store is an immutable object store keyed by forward-slash separated keys.
type Store interface {
	// Put writes the object at key, reading its contents from r. Parent
	// "directories" implied by the key are created as needed. Writes are
	// atomic: a reader never observes a partially written object.
	Put(ctx context.Context, key string, r io.Reader) error

	// Get returns a reader for the entire object at key. It returns ErrNotFound
	// if the key does not exist. The caller must Close the returned reader.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// GetRange returns a reader for size bytes of the object at key starting at
	// offset. It returns ErrNotFound if the key does not exist. The caller must
	// Close the returned reader.
	GetRange(ctx context.Context, key string, offset, size int64) (io.ReadCloser, error)

	// List returns all object keys under prefix, recursively. Keys are returned
	// relative to the store root using forward-slash separators.
	List(ctx context.Context, prefix string) ([]string, error)

	// Delete removes the object at key. Deleting a non-existent key returns nil.
	Delete(ctx context.Context, key string) error

	// Exists reports whether an object exists at key.
	Exists(ctx context.Context, key string) (bool, error)

	// Close releases any resources held by the store.
	Close() error
}
