package local

import (
	"fmt"
	"syscall"
	"testing"
)

func TestIsUnsupported(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"enotsup", syscall.ENOTSUP, true},
		{"eopnotsupp", syscall.EOPNOTSUPP, true},
		{"enosys", syscall.ENOSYS, true},
		{"einval", syscall.EINVAL, true},
		{"wrapped enotsup", fmt.Errorf("sync: %w", syscall.ENOTSUP), true},
		{"eio not tolerated", syscall.EIO, false},
		{"eacces not tolerated", syscall.EACCES, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUnsupported(tc.err); got != tc.want {
				t.Fatalf("isUnsupported(%v) = %v; want %v", tc.err, got, tc.want)
			}
		})
	}
}
