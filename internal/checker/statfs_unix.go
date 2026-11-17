//go:build !windows

package checker

import "syscall"

// diskUsage 返回路径所在文件系统的总容量与剩余字节数（unix 实现）。
func diskUsage(path string) (total, free uint64, err error) {
	st := &syscall.Statfs_t{}
	if err := syscall.Statfs(path, st); err != nil {
		return 0, 0, err
	}
	total = st.Blocks * uint64(st.Bsize)
	free = st.Bavail * uint64(st.Bsize) // 非特权用户可用空间
	return total, free, nil
}
