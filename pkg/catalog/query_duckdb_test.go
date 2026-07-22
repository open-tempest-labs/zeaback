//go:build duckdb_arrow

package catalog_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/store/local"
)

func TestDuckDBQuery(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := local.New(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer s.Close()
	c := catalog.New(s)

	ts := time.Unix(1_700_000_000, 0).UTC()
	if err := c.WriteSnapshotRecord(ctx, catalog.Snapshot{ID: "snapQ", Timestamp: ts, EventLabel: "pre-deploy"}); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	nodes := []catalog.Node{
		{Path: "big.bin", Type: catalog.NodeFile, Size: 1000, MTime: ts},
		{Path: "small.txt", Type: catalog.NodeFile, Size: 10, MTime: ts},
	}
	if err := c.WriteNodes(ctx, "snapQ", nodes); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	var buf bytes.Buffer
	if err := catalog.Query(dir, "SELECT id, event_label FROM snapshots", &buf); err != nil {
		t.Fatalf("query snapshots: %v", err)
	}
	if !strings.Contains(buf.String(), "snapQ") || !strings.Contains(buf.String(), "pre-deploy") {
		t.Fatalf("snapshot query output missing data:\n%s", buf.String())
	}

	buf.Reset()
	if err := catalog.Query(dir, "SELECT path FROM nodes WHERE type='file' ORDER BY size DESC LIMIT 1", &buf); err != nil {
		t.Fatalf("query nodes: %v", err)
	}
	if !strings.Contains(buf.String(), "big.bin") {
		t.Fatalf("nodes query output missing data:\n%s", buf.String())
	}
}
