//go:build volumez

// Package volumez adapts a volumez storage backend to zeaback's store.Store
// interface, letting zeaback write its repository directly to any backend
// volumez supports (S3, HTTP, ...) without a FUSE mount.
//
// Because zeaback only writes immutable, content-addressed objects, volumez's
// whole-object read-modify-write behavior never applies: every object is written
// exactly once.
package volumez

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	vbackend "github.com/lmccay/volumez/pkg/backend"

	"github.com/open-tempest-labs/zeaback/pkg/store"
)

// Store implements store.Store over a volumez backend.
type Store struct {
	b vbackend.Backend
}

// New creates a volumez-backed store using the named backend (e.g. "s3",
// "http") and its configuration.
func New(backendName string, cfg map[string]any) (*Store, error) {
	b, err := vbackend.Create(backendName, cfg)
	if err != nil {
		return nil, fmt.Errorf("volumez: create backend %q: %w", backendName, err)
	}
	return &Store{b: b}, nil
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, vbackend.ErrNotFound) {
		return store.ErrNotFound
	}
	return err
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader) error {
	if err := s.b.WriteFileStream(ctx, key, r, 0o644); err != nil {
		return fmt.Errorf("volumez: put %q: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	data, err := s.b.ReadFile(ctx, key)
	if err != nil {
		return nil, mapErr(err)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *Store) GetRange(ctx context.Context, key string, offset, size int64) (io.ReadCloser, error) {
	data, err := s.b.ReadFileRange(ctx, key, offset, size)
	if err != nil {
		return nil, mapErr(err)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := s.b.ListDir(ctx, dir)
		if err != nil {
			if errors.Is(err, vbackend.ErrNotFound) {
				return nil
			}
			return err
		}
		for _, fi := range entries {
			child := path.Join(dir, fi.Name)
			if fi.IsDir {
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			out = append(out, child)
		}
		return nil
	}
	if err := walk(prefix); err != nil {
		return nil, fmt.Errorf("volumez: list %q: %w", prefix, err)
	}
	return out, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.b.Delete(ctx, key); err != nil && !errors.Is(err, vbackend.ErrNotFound) {
		return fmt.Errorf("volumez: delete %q: %w", key, err)
	}
	return nil
}

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	ok, err := s.b.Exists(ctx, key)
	if err != nil {
		return false, mapErr(err)
	}
	return ok, nil
}

func (s *Store) Close() error { return s.b.Close() }
