//go:build !windows

package main

import "syscall"

// diskStats возвращает свободное и полное место на диске с путём.
func diskStats(path string) (free, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bs := uint64(st.Bsize)
	return uint64(st.Bavail) * bs, uint64(st.Blocks) * bs
}
