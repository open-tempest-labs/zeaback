// Package repo ties a store together with its metadata catalog to form a
// zeaback repository: the durable home for a backup's data packs and Parquet
// manifests.
package repo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/open-tempest-labs/zeaback/pkg/cas"
	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/store"
)

const (
	formatVersion = 1
	metaKey       = "repo.json"
)

// Meta is the repository descriptor stored at repo.json.
type Meta struct {
	ID            string             `json:"id"`
	FormatVersion int                `json:"format_version"`
	Created       time.Time          `json:"created"`
	Chunker       cas.ChunkerOptions `json:"chunker"`
}

// Repository is an initialized zeaback repository.
type Repository struct {
	store    store.Store
	cat      *catalog.Catalog
	meta     Meta
	localDir string
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Init creates a new repository in s. localDir, if non-empty, is the filesystem
// directory backing s (used to enable the DuckDB query engine).
func Init(ctx context.Context, s store.Store, localDir string) (*Repository, error) {
	if ok, err := s.Exists(ctx, metaKey); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("repo: already initialized")
	}
	m := Meta{
		ID:            randomID(),
		FormatVersion: formatVersion,
		Created:       time.Now().UTC(),
		Chunker:       cas.DefaultChunkerOptions,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("repo: marshal meta: %w", err)
	}
	if err := s.Put(ctx, metaKey, bytes.NewReader(append(data, '\n'))); err != nil {
		return nil, fmt.Errorf("repo: write meta: %w", err)
	}
	return &Repository{store: s, cat: catalog.New(s), meta: m, localDir: localDir}, nil
}

// Open opens an existing repository in s.
func Open(ctx context.Context, s store.Store, localDir string) (*Repository, error) {
	rc, err := s.Get(ctx, metaKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("repo: not initialized (run 'zeaback init')")
		}
		return nil, err
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("repo: read meta: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("repo: parse meta: %w", err)
	}
	if m.Chunker.Avg == 0 {
		m.Chunker = cas.DefaultChunkerOptions
	}
	return &Repository{store: s, cat: catalog.New(s), meta: m, localDir: localDir}, nil
}

// Store returns the underlying object store.
func (r *Repository) Store() store.Store { return r.store }

// Catalog returns the metadata catalog.
func (r *Repository) Catalog() *catalog.Catalog { return r.cat }

// Meta returns the repository descriptor.
func (r *Repository) Meta() Meta { return r.meta }

// LocalDir returns the filesystem directory backing the repository, or "" if the
// store is not local (in which case the DuckDB query engine is unavailable).
func (r *Repository) LocalDir() string { return r.localDir }

// Close releases the repository's resources.
func (r *Repository) Close() error { return r.store.Close() }
