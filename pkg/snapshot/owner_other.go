//go:build !unix

package snapshot

import "io/fs"

// fileOwner returns zero uid/gid on non-unix systems.
func fileOwner(info fs.FileInfo) (uint32, uint32) { return 0, 0 }
