// Package local implements a store.Store backed by a local filesystem directory.
//
// Pointing the root at a mounted volumez FUSE path is how a local store targets
// cloud/external storage with zero code coupling to volumez.
package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/open-tempest-labs/zeaback/pkg/store"
)

// Store is a filesystem-backed object store rooted at a directory.
type Store struct {
	root string
}

// New returns a local store rooted at dir, creating the directory if needed.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("local: empty root directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("local: resolve root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("local: create root: %w", err)
	}
	return &Store{root: abs}, nil
}

// path maps a forward-slash key to an absolute filesystem path.
func (s *Store) path(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(key))
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader) error {
	dst := s.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("local: mkdir for %q: %w", key, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return fmt.Errorf("local: temp for %q: %w", key, err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("local: write %q: %w", key, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("local: sync %q: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("local: close %q: %w", key, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("local: rename %q: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(s.path(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("local: open %q: %w", key, err)
	}
	return f, nil
}

// rangeReader limits reads to a byte window and closes the underlying file.
type rangeReader struct {
	f *os.File
	r io.Reader
}

func (rr *rangeReader) Read(p []byte) (int, error) { return rr.r.Read(p) }
func (rr *rangeReader) Close() error               { return rr.f.Close() }

func (s *Store) GetRange(ctx context.Context, key string, offset, size int64) (io.ReadCloser, error) {
	f, err := os.Open(s.path(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("local: open %q: %w", key, err)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return nil, fmt.Errorf("local: seek %q: %w", key, err)
	}
	return &rangeReader{f: f, r: io.LimitReader(f, size)}, nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	base := s.path(prefix)
	var keys []string
	err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // empty prefix -> no keys
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".tmp-") {
			return nil // skip in-flight temp files
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("local: list %q: %w", prefix, err)
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := os.Remove(s.path(key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("local: delete %q: %w", key, err)
	}
	return nil
}

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := os.Stat(s.path(key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("local: stat %q: %w", key, err)
}

func (s *Store) Close() error { return nil }
