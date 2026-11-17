//go:build !windows

package workerd

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/VaalaCat/frp-panel/utils/logger"
	"golang.org/x/net/context"
)

// exec_unix.go workerd 降权运行（安全审计 P1-12，决策点 F）：
// workerd 执行租户 JS 代码，此前与 frpp 同权限（root systemd 服务）运行。
// 这里在 unix 上让 workerd 进程落到专用用户 frpp-worker 下；
// 用户不存在时保留原行为并告警（部署脚本 install.sh 会创建该用户）。

const lowerPrivUser = "frpp-worker"

var (
	lowerPrivOnce sync.Once
	lowerPrivUID  uint32
	lowerPrivGID  uint32
	lowerPrivOK   bool
)

func lookupLowerPrivUser() (uid, gid uint32, ok bool) {
	lowerPrivOnce.Do(func() {
		u, err := user.Lookup(lowerPrivUser)
		if err != nil {
			return
		}
		uidN, err1 := strconv.Atoi(u.Uid)
		gidN, err2 := strconv.Atoi(u.Gid)
		if err1 != nil || err2 != nil {
			return
		}
		lowerPrivUID, lowerPrivGID, lowerPrivOK = uint32(uidN), uint32(gidN), true
	})
	return lowerPrivUID, lowerPrivGID, lowerPrivOK
}

// applyLowerPrivilege 让 workerd 进程以 frpp-worker 身份运行；用户不存在时跳过。
func applyLowerPrivilege(cmd *exec.Cmd) {
	uid, gid, ok := lookupLowerPrivUser()
	if !ok {
		logger.Logger(context.Background()).Warnf("lower-privilege user [%s] not found, workerd will run with frpp's privileges", lowerPrivUser)
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uid, Gid: gid}
}

// chownTree 把 worker 目录递归交给降权用户（workerd 需要在其中读写代码与配置）。
func chownTree(dir string) {
	uid, gid, ok := lookupLowerPrivUser()
	if !ok {
		return
	}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if chownErr := os.Chown(path, int(uid), int(gid)); chownErr != nil {
			logger.Logger(context.Background()).Warnf("chown [%s] failed: %v", path, chownErr)
		}
		return nil
	})
}
