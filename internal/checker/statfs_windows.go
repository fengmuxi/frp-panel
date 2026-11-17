//go:build windows

package checker

import "errors"

// diskUsage 在 windows 上不支持 POSIX statfs，调用方按平台 Skip。
func diskUsage(path string) (total, free uint64, err error) {
	return 0, 0, errors.New("disk usage probe not supported on windows")
}
