package workerd

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/samber/lo"
)

type Opt func(*pb.Worker)

func FillWorkerValue(worker *pb.Worker, UserID uint, opt ...Opt) {

	worker.UserId = lo.ToPtr(uint32(UserID))

	if len(worker.GetName()) == 0 {
		worker.Name = lo.ToPtr(utils.NewCodeName(2))
	}

	if len(worker.GetCode()) == 0 {
		worker.Code = lo.ToPtr(string(defs.DefaultCode))
	}
	if len(worker.GetWorkerId()) == 0 {
		worker.WorkerId = lo.ToPtr(utils.GenerateUUID())
	}
	if len(worker.GetCodeEntry()) == 0 {
		worker.CodeEntry = lo.ToPtr(string(defs.DefaultEntry))
	}
	if len(worker.GetConfigTemplate()) == 0 {
		worker.ConfigTemplate = lo.ToPtr(string(defs.DefaultConfigTemplate))
	}

	worker.Socket = &pb.Socket{
		Name:    lo.ToPtr(worker.GetWorkerId()),
		Address: lo.ToPtr(fmt.Sprintf(defs.DefaultSocketTemplate, worker.GetWorkerId())),
	}

	for _, o := range opt {
		o(worker)
	}
}

func SafeWorkerID(id string) string {
	replacer := strings.NewReplacer("/", "", ".", "", "-", "")
	return replacer.Replace(id)
}

// workerPathSegRe 限制 worker 路径段的字符集（安全审计 P0-2：
// WorkerId/CodeEntry 曾被原样拼进文件路径，`../` 即可在节点上以运行身份写任意文件）。
// 允许字母数字与 _ . / -，显式排除 ".."、绝对路径与反斜杠。
var workerPathSegRe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// ValidateWorkerPathSegment 校验单个路径段/相对路径是否安全（供 API 层快速拒绝）。
func ValidateWorkerPathSegment(seg string) bool {
	// Clean 归一化后必须与原值（统一为 / 分隔）一致：排除 "."、".."、重复分隔符等变形
	if seg == "" || filepath.ToSlash(seg) != filepath.ToSlash(filepath.Clean(seg)) {
		return false
	}
	return workerPathSegRe.MatchString(seg) &&
		!strings.Contains(seg, "..") &&
		!strings.HasPrefix(seg, "/") &&
		!strings.HasPrefix(seg, "\\") &&
		!filepath.IsAbs(seg)
}

// EnsureWorkerPathsSafe 在任何写盘/清理动作前校验 worker 的路径字段，
// 并断言解析后的路径仍然位于 workerdCWD 之内（containment 双保险）。
func EnsureWorkerPathsSafe(ctx context.Context, worker *pb.Worker, workerdCWD string) error {
	for name, seg := range map[string]string{
		"workerId":   worker.GetWorkerId(),
		"codeEntry":  worker.GetCodeEntry(),
	} {
		if !ValidateWorkerPathSegment(seg) {
			return fmt.Errorf("invalid worker %s: [%s]", name, seg)
		}
	}
	for _, p := range []string{
		WorkerCWDPath(ctx, worker, workerdCWD),
		WorkerCodeRootPath(ctx, worker, workerdCWD),
		CodeFilePath(ctx, worker, workerdCWD),
		ConfigFilePath(ctx, worker, workerdCWD),
	} {
		if err := ensureUnderCwd(workerdCWD, p); err != nil {
			return err
		}
	}
	return nil
}

// ensureUnderCwd 断言 target 解析后仍在 base 目录内部（防前缀式检查被绕过）。
func ensureUnderCwd(base, target string) error {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return fmt.Errorf("resolve base [%s]: %w", base, err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve target [%s]: %w", target, err)
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path [%s] escapes workerd cwd [%s]", target, base)
	}
	return nil
}
