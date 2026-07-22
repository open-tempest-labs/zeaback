package catalog

import (
	"path"
	"strings"
)

// StoredPath translates a user-supplied path into the basename-relative form
// used inside a snapshot's node tree.
//
// Backups store paths relative to each source root's basename (backing up
// /home/me/Documents stores "Documents/..."). A user naturally refers to files
// by the absolute path they backed up, so an absolute path is matched against the
// snapshot's source roots and rewritten to the stored form. A non-absolute value
// is assumed to already be a stored path and returned as-is. If no source root
// matches, the cleaned input is returned (it simply won't match any node).
func StoredPath(snap Snapshot, userPath string) string {
	if userPath == "" {
		return ""
	}
	u := path.Clean(strings.ReplaceAll(userPath, "\\", "/"))
	if !strings.HasPrefix(u, "/") {
		return strings.TrimSuffix(u, "/")
	}
	for _, s := range snap.SourcePaths {
		root := path.Clean(strings.ReplaceAll(s, "\\", "/"))
		if !strings.HasPrefix(root, "/") {
			continue
		}
		base := path.Base(root)
		if u == root {
			return base
		}
		if strings.HasPrefix(u, root+"/") {
			return base + "/" + strings.TrimPrefix(u, root+"/")
		}
	}
	return strings.TrimPrefix(u, "/")
}

// StoredRoots returns the basename-relative roots of a snapshot's sources, for
// user-facing hints (e.g. "paths are relative to: Documents").
func StoredRoots(snap Snapshot) []string {
	seen := map[string]bool{}
	var roots []string
	for _, s := range snap.SourcePaths {
		b := path.Base(path.Clean(strings.ReplaceAll(s, "\\", "/")))
		if b != "" && b != "." && b != "/" && !seen[b] {
			seen[b] = true
			roots = append(roots, b)
		}
	}
	return roots
}
