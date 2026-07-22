package compact_test

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-tempest-labs/zeaback/pkg/catalog"
	"github.com/open-tempest-labs/zeaback/pkg/compact"
	"github.com/open-tempest-labs/zeaback/pkg/repo"
	"github.com/open-tempest-labs/zeaback/pkg/restore"
	"github.com/open-tempest-labs/zeaback/pkg/snapshot"
	"github.com/open-tempest-labs/zeaback/pkg/store/local"
	"github.com/open-tempest-labs/zeaback/pkg/verify"
)

func data(seed int64, n int) []byte {
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

func TestCompactReclaimsForgotten(t *testing.T) {
	ctx := context.Background()

	src := t.TempDir()
	base := filepath.Base(src)
	v1 := data(11, 400*1024)
	if err := os.WriteFile(filepath.Join(src, "big.bin"), v1, 0o644); err != nil {
		t.Fatal(err)
	}

	repoDir := t.TempDir()
	s, _ := local.New(repoDir)
	r, err := repo.Init(ctx, s, repoDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	snap1, _, err := snapshot.Create(ctx, r, snapshot.Options{SourcePaths: []string{src}})
	if err != nil {
		t.Fatalf("backup1: %v", err)
	}

	// Replace the file with entirely different content and back up again.
	v2 := data(22, 450*1024)
	if err := os.WriteFile(filepath.Join(src, "big.bin"), v2, 0o644); err != nil {
		t.Fatal(err)
	}
	snap2, _, err := snapshot.Create(ctx, r, snapshot.Options{SourcePaths: []string{src}})
	if err != nil {
		t.Fatalf("backup2: %v", err)
	}

	// Forget snapshot 1 — its exclusive chunks become dead.
	if err := r.Catalog().DeleteSnapshot(ctx, snap1.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}

	st, err := compact.Run(ctx, r, compact.Options{})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if st.PacksDeleted == 0 && st.PacksRewritten == 0 {
		t.Fatalf("compaction reclaimed nothing: %+v", st)
	}
	if st.DeadChunks == 0 {
		t.Fatalf("expected dead chunks after forget: %+v", st)
	}
	if st.PacksAfter >= st.PacksBefore && st.BytesReclaimed <= 0 {
		t.Fatalf("compaction did not shrink repo: %+v", st)
	}

	// Repository must still be consistent and snapshot 2 fully restorable.
	if _, err := verify.Run(ctx, r, verify.Options{ReadBlobs: true}); err != nil {
		t.Fatalf("verify after compact: %v", err)
	}
	dest := t.TempDir()
	if _, err := restore.Run(ctx, r, restore.Options{Selector: catalog.Selector{ID: snap2.ID}, Dest: dest}); err != nil {
		t.Fatalf("restore after compact: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, base, "big.bin"))
	if err != nil || !bytes.Equal(got, v2) {
		t.Fatalf("restored content wrong after compact (err=%v)", err)
	}
}
