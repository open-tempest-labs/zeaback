//go:build unix

package snapshot

import (
	"io/fs"
	"syscall"
)

// fileOwner returns the uid and gid of a file on unix systems.
func fileOwner(info fs.FileInfo) (uint32, uint32) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Uid, st.Gid
	}
	return 0, 0
}
