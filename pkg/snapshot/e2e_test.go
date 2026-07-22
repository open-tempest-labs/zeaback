package snapshot_test

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
	"github.com/open-tempest-labs/zeaback/pkg/restore"
	"github.com/open-tempest-labs/zeaback/pkg/snapshot"
	"github.com/open-tempest-labs/zeaback/pkg/store/local"
)

func bigData(seed int64, n int) []byte {
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	for i := range b {
		if r.Intn(5) == 0 {
			b[i] = byte(r.Intn(256))
		} else {
			b[i] = byte(i % 251)
		}
	}
	return b
}

func openRepo(t *testing.T, dir string, init bool) *repo.Repository {
	t.Helper()
	s, err := local.New(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	var r *repo.Repository
	if init {
		r, err = repo.Init(context.Background(), s, dir)
	} else {
		r, err = repo.Open(context.Background(), s, dir)
	}
	if err != nil {
		t.Fatalf("repo: %v", err)
	}
	return r
}

func TestBackupRestoreIncremental(t *testing.T) {
	ctx := context.Background()

	src := t.TempDir()
	base := filepath.Base(src)
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	aData := []byte("original contents of a\n")
	bData := bigData(7, 512*1024)
	if err := os.WriteFile(filepath.Join(src, "a.txt"), aData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.bin"), bData, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink("a.txt", filepath.Join(src, "link"))

	repoDir := t.TempDir()
	r := openRepo(t, repoDir, true)

	// Backup 1.
	snap1, st1, err := snapshot.Create(ctx, r, snapshot.Options{SourcePaths: []string{src}, EventLabel: "first"})
	if err != nil {
		t.Fatalf("backup1: %v", err)
	}
	if st1.Files < 2 || st1.NewChunks == 0 {
		t.Fatalf("backup1 stats unexpected: %+v", st1)
	}

	// Restore whole tree and compare.
	dest1 := t.TempDir()
	if _, err := restore.Run(ctx, r, restore.Options{Selector: catalog.Selector{ID: snap1.ID}, Dest: dest1}); err != nil {
		t.Fatalf("restore1: %v", err)
	}
	assertFile(t, filepath.Join(dest1, base, "a.txt"), aData)
	assertFile(t, filepath.Join(dest1, base, "sub", "b.bin"), bData)
	if target, err := os.Readlink(filepath.Join(dest1, base, "link")); err != nil || target != "a.txt" {
		t.Fatalf("symlink restore: %q, %v", target, err)
	}

	// Modify a.txt only; b.bin unchanged.
	aData2 := []byte("MODIFIED contents of a, now longer than before\n")
	if err := os.WriteFile(filepath.Join(src, "a.txt"), aData2, 0o644); err != nil {
		t.Fatal(err)
	}

	// Backup 2 (incremental) — reopen repo to prove persistence.
	r2 := openRepo(t, repoDir, false)
	snap2, st2, err := snapshot.Create(ctx, r2, snapshot.Options{SourcePaths: []string{src}, EventLabel: "second"})
	if err != nil {
		t.Fatalf("backup2: %v", err)
	}
	if snap2.ParentID != snap1.ID {
		t.Fatalf("backup2 parent = %q; want %q", snap2.ParentID, snap1.ID)
	}
	if st2.ReusedChunks == 0 {
		t.Fatalf("backup2 reused no chunks; incremental dedup failed: %+v", st2)
	}
	if st2.NewChunks >= st1.NewChunks {
		t.Fatalf("backup2 new chunks %d not fewer than backup1 %d", st2.NewChunks, st1.NewChunks)
	}

	// Point-in-time restore of snapshot 1 must yield the ORIGINAL a.txt.
	destOld := t.TempDir()
	atFirst := snap1.Timestamp
	if _, err := restore.Run(ctx, r2, restore.Options{Selector: catalog.Selector{At: &atFirst}, Dest: destOld}); err != nil {
		t.Fatalf("restore old: %v", err)
	}
	assertFile(t, filepath.Join(destOld, base, "a.txt"), aData)

	// Latest restore must yield the MODIFIED a.txt and unchanged b.bin.
	destNew := t.TempDir()
	if _, err := restore.Run(ctx, r2, restore.Options{Selector: catalog.Selector{ID: "latest"}, Dest: destNew}); err != nil {
		t.Fatalf("restore latest: %v", err)
	}
	assertFile(t, filepath.Join(destNew, base, "a.txt"), aData2)
	assertFile(t, filepath.Join(destNew, base, "sub", "b.bin"), bData)

	// Event-based restore.
	destEvent := t.TempDir()
	if _, err := restore.Run(ctx, r2, restore.Options{Selector: catalog.Selector{Event: "first"}, Dest: destEvent}); err != nil {
		t.Fatalf("restore event: %v", err)
	}
	assertFile(t, filepath.Join(destEvent, base, "a.txt"), aData)

	// File-level restore: just sub/b.bin.
	destFile := t.TempDir()
	stf, err := restore.Run(ctx, r2, restore.Options{
		Selector:   catalog.Selector{ID: "latest"},
		PathPrefix: base + "/sub/b.bin",
		Dest:       destFile,
	})
	if err != nil {
		t.Fatalf("restore file: %v", err)
	}
	if stf.Files != 1 {
		t.Fatalf("file-level restore touched %d files; want 1", stf.Files)
	}
	assertFile(t, filepath.Join(destFile, base, "sub", "b.bin"), bData)
	if _, err := os.Stat(filepath.Join(destFile, base, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("file-level restore leaked a.txt")
	}
}

func assertFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch at %s: got %d bytes, want %d", path, len(got), len(want))
	}
}
