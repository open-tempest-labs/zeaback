package catalog_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/store/local"
)

func newCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	s, err := local.New(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return catalog.New(s)
}

func TestSnapshotRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := newCatalog(t)

	ts := time.Unix(1_700_000_000, 0).UTC()
	snap := catalog.Snapshot{
		ID:          "snap-1",
		Timestamp:   ts,
		EventLabel:  "pre-deploy",
		Tags:        map[string]string{"host": "laptop", "job": "nightly"},
		Host:        "laptop",
		SourcePaths: []string{"/src/projects"},
		Annotations: map[string]string{},
	}
	if err := c.WriteSnapshotRecord(ctx, snap); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	nodes := []catalog.Node{
		{Path: "a.txt", Type: catalog.NodeFile, Mode: 0o644, Size: 12, MTime: ts, ContentHash: "h1", Chunks: []string{"c1", "c2"}},
		{Path: "sub", Type: catalog.NodeDir, Mode: 0o755, MTime: ts},
	}
	if err := c.WriteNodes(ctx, "snap-1", nodes); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	chunks := []catalog.ChunkEntry{
		{Hash: "c1", PackID: "p1", Offset: 8, CompLength: 20, Length: 40},
		{Hash: "c2", PackID: "p1", Offset: 28, CompLength: 10, Length: 15},
	}
	packs := []catalog.PackEntry{{PackID: "p1", Size: 512, ChunkCount: 2}}
	if err := c.WriteBackupManifests(ctx, "snap-1", chunks, packs); err != nil {
		t.Fatalf("write backup manifests: %v", err)
	}

	// Snapshots
	snaps, err := c.LoadSnapshots(ctx)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("load snapshots = %v, %v", snaps, err)
	}
	got := snaps[0]
	if got.ID != "snap-1" || got.EventLabel != "pre-deploy" || !got.Timestamp.Equal(ts) {
		t.Fatalf("snapshot mismatch: %+v", got)
	}
	if got.Tags["job"] != "nightly" || len(got.SourcePaths) != 1 {
		t.Fatalf("snapshot nested fields mismatch: %+v", got)
	}
	// A record written without an explicit kind reads back as the default.
	if got.Kind != catalog.KindBackup {
		t.Fatalf("default kind = %q; want %q", got.Kind, catalog.KindBackup)
	}

	// Nodes
	gotNodes, err := c.LoadNodes(ctx, "snap-1")
	if err != nil || len(gotNodes) != 2 {
		t.Fatalf("load nodes = %d, %v", len(gotNodes), err)
	}
	if gotNodes[0].Path != "a.txt" || len(gotNodes[0].Chunks) != 2 || gotNodes[0].Chunks[1] != "c2" {
		t.Fatalf("node mismatch: %+v", gotNodes[0])
	}
	if gotNodes[1].Type != catalog.NodeDir {
		t.Fatalf("dir node mismatch: %+v", gotNodes[1])
	}

	// Chunk index
	idx, err := c.LoadChunkIndex(ctx)
	if err != nil || len(idx) != 2 {
		t.Fatalf("chunk index = %d, %v", len(idx), err)
	}
	if idx["c1"].PackID != "p1" || idx["c1"].Length != 40 {
		t.Fatalf("chunk entry mismatch: %+v", idx["c1"])
	}

	// Pack index
	pidx, err := c.LoadPackIndex(ctx)
	if err != nil || len(pidx) != 1 || pidx[0].ChunkCount != 2 {
		t.Fatalf("pack index = %+v, %v", pidx, err)
	}

	// Live index
	li, err := c.LoadLiveIndex(ctx)
	if err != nil || !li.Has("c1") || li.Has("nope") {
		t.Fatalf("live index unexpected: %v", err)
	}
}

func TestSnapshotKindActorRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := newCatalog(t)

	ts := time.Unix(1_700_000_100, 0).UTC()
	if err := c.WriteSnapshotRecord(ctx, catalog.Snapshot{
		ID: "sess-1", Timestamp: ts,
		Kind: catalog.KindAgentSession, Actor: "agent:claude-opus",
		EventLabel:  "refactor-auth",
		Annotations: map[string]string{"intent": "tighten JWT checks", "session_id": "abc123"},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	snaps, err := c.LoadSnapshots(ctx)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("load = %v, %v", snaps, err)
	}
	got := snaps[0]
	if got.Kind != catalog.KindAgentSession || got.Actor != "agent:claude-opus" {
		t.Fatalf("kind/actor mismatch: %+v", got)
	}
	if got.Annotations["intent"] != "tighten JWT checks" || got.Annotations["session_id"] != "abc123" {
		t.Fatalf("annotations mismatch: %+v", got.Annotations)
	}
}

// TestNodeManifestLargeVaried guards the Parquet encoding path against the
// "dict eof exception" class of failures: many high-cardinality rows plus a few
// rows carrying very large chunk-list cells (as a multi-GB file would). It must
// round-trip exactly.
func TestNodeManifestLargeVaried(t *testing.T) {
	ctx := context.Background()
	c := newCatalog(t)

	hash := func(i int) string { return fmt.Sprintf("%064x", i) }
	var nodes []catalog.Node
	next := 0
	for i := 0; i < 3000; i++ {
		n := catalog.Node{Path: fmt.Sprintf("dir%d/file%d.dat", i%50, i), Type: catalog.NodeFile}
		// A handful of rows get huge chunk lists (~25k hashes ≈ 1.6 MB cell).
		count := 1
		if i%600 == 0 {
			count = 25000
		}
		for j := 0; j < count; j++ {
			n.Chunks = append(n.Chunks, hash(next))
			next++
		}
		nodes = append(nodes, n)
	}
	if err := c.WriteNodes(ctx, "big", nodes); err != nil {
		t.Fatalf("write nodes: %v", err)
	}
	got, err := c.LoadNodes(ctx, "big")
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(got) != len(nodes) {
		t.Fatalf("node count = %d; want %d", len(got), len(nodes))
	}
	// Spot-check a huge-cell row round-tripped exactly.
	if len(got[600].Chunks) != 25000 || got[600].Chunks[24999] == "" {
		t.Fatalf("huge chunk-list row corrupted: len=%d", len(got[600].Chunks))
	}
	if got[1].Path != nodes[1].Path || got[1].Chunks[0] != nodes[1].Chunks[0] {
		t.Fatalf("row mismatch: %+v", got[1])
	}
}

func TestStoredPath(t *testing.T) {
	// StoredPath is pure string translation (no filesystem access); the paths
	// below are synthetic and machine-independent.
	snap := catalog.Snapshot{SourcePaths: []string{"/src/root/Documents"}}
	cases := map[string]string{
		"/src/root/Documents":           "Documents",          // source root
		"/src/root/Documents/Personal":  "Documents/Personal", // under root
		"/src/root/Documents/Personal/": "Documents/Personal", // trailing slash
		"Documents/Personal":            "Documents/Personal", // already stored form
		"/somewhere/else":               "somewhere/else",     // outside any source
		"":                              "",
	}
	for in, want := range cases {
		if got := catalog.StoredPath(snap, in); got != want {
			t.Errorf("StoredPath(%q) = %q; want %q", in, got, want)
		}
	}
	if roots := catalog.StoredRoots(snap); len(roots) != 1 || roots[0] != "Documents" {
		t.Errorf("StoredRoots = %v; want [Documents]", roots)
	}
}

func TestResolve(t *testing.T) {
	t0 := time.Unix(1000, 0).UTC()
	snaps := []catalog.Snapshot{
		{ID: "aaa111", Timestamp: t0},
		{ID: "bbb222", Timestamp: t0.Add(time.Hour), EventLabel: "pre-deploy"},
		{ID: "ccc333", Timestamp: t0.Add(2 * time.Hour)},
		{ID: "ddd444", Timestamp: t0.Add(3 * time.Hour), EventLabel: "pre-deploy"},
	}

	// latest
	if s, _ := catalog.Resolve(snaps, catalog.Selector{ID: "latest"}); s.ID != "ddd444" {
		t.Fatalf("latest = %s", s.ID)
	}
	// explicit + prefix
	if s, _ := catalog.Resolve(snaps, catalog.Selector{ID: "ccc333"}); s.ID != "ccc333" {
		t.Fatalf("by id = %s", s.ID)
	}
	if s, _ := catalog.Resolve(snaps, catalog.Selector{ID: "bbb2"}); s.ID != "bbb222" {
		t.Fatalf("by prefix = %s", s.ID)
	}
	// point-in-time: at 2.5h -> ccc333
	at := t0.Add(150 * time.Minute)
	if s, _ := catalog.Resolve(snaps, catalog.Selector{At: &at}); s.ID != "ccc333" {
		t.Fatalf("at = %s", s.ID)
	}
	// event: latest pre-deploy -> ddd444
	if s, _ := catalog.Resolve(snaps, catalog.Selector{Event: "pre-deploy"}); s.ID != "ddd444" {
		t.Fatalf("event = %s", s.ID)
	}
	// event before 2h -> bbb222
	before := t0.Add(2 * time.Hour)
	if s, _ := catalog.Resolve(snaps, catalog.Selector{Event: "pre-deploy", Before: &before}); s.ID != "bbb222" {
		t.Fatalf("event before = %s", s.ID)
	}
	// missing
	if _, err := catalog.Resolve(snaps, catalog.Selector{ID: "zzz"}); err == nil {
		t.Fatalf("expected error for missing id")
	}
}
