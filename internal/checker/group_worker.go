package checker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/utils"
	"google.golang.org/protobuf/proto"
)

func buildWorkerGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&workerDBIntegrity{baseChecker{name: "db_integrity", group: "worker", severity: LevelMajor,
			fixHint: "workers.socket 应为合法 JSON；若记录损坏请在面板中重建 Worker"}},
		&workerBinary{baseChecker{name: "workerd_binary", group: "worker", severity: LevelMajor,
			fixHint: "安装 workerd：面板 `安装 workerd` 按钮或 CLIENT_WORKER_WORKERD_DOWNLOAD_URL_* 指定的镜像地址"}},
		&workerWorkdirFiles{baseChecker{name: "workdir_files", group: "worker", severity: LevelMinor,
			fixHint: "磁盘上的 worker 代码与数据库不一致时，在面板中重新部署（redeploy）该 Worker 即可覆盖"}},
		&workerIngressRefs{baseChecker{name: "ingress_refs", group: "worker", severity: LevelMajor,
			fixHint: "隧道 annotations 引用了不存在的 worker_id，该隧道的 ingress 路由不会生效"}},
	}, nil
}

// loadWorkers 读取 workers 表（只读）。
func loadWorkers(ctx context.Context, env *Env) ([]models.Worker, error) {
	db, err := env.DB(ctx)
	if err != nil {
		return nil, err
	}
	var rows []models.Worker
	if err := db.Model(&models.Worker{}).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query workers: %w", err)
	}
	return rows, nil
}

// ---------------- worker/db_integrity ----------------

type workerDBIntegrity struct{ baseChecker }

func (c *workerDBIntegrity) Run(ctx context.Context, env *Env) Result {
	refs := []string{"workers 表", "workers.socket(JSON)", "worker_clients 关联表"}
	rows, err := loadWorkers(ctx, env)
	if err != nil {
		if env.HasDB(ctx) {
			return Result{}.Fail(fmt.Sprintf("读取 workers 表失败: %v", err))
		}
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(rows) == 0 {
		return Result{}.Skip("workers 表为空（未部署 Worker 或 CLIENT_FEATURES_ENABLE_FUNCTIONS=false）")
	}

	var bad []string
	for _, w := range rows {
		if !env.NodeFilter(w.ID) {
			continue
		}
		if w.Socket.Data == nil {
			bad = append(bad, fmt.Sprintf("worker=%s socket 配置为空", w.ID))
			continue
		}
		if proto.Size(w.Socket.Data) == 0 {
			bad = append(bad, fmt.Sprintf("worker=%s socket 配置异常", w.ID))
		}
		if w.CodeEntry == "" {
			bad = append(bad, fmt.Sprintf("worker=%s code_entry 为空", w.ID))
		}
	}
	if len(bad) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d/%d 个 Worker 记录异常", len(bad), len(rows))).WithDetails(bad...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 个 Worker 记录结构完整", len(rows))).
		WithDetails(fmt.Sprintf("socket 模板: %s", defs.DefaultSocketTemplate)).ConfigRefs(refs...)
}

// ---------------- worker/workerd_binary ----------------

type workerBinary struct{ baseChecker }

func (c *workerBinary) Run(ctx context.Context, env *Env) Result {
	refs := []string{"CLIENT_WORKER_WORKERD_BINARY_PATH", "CLIENT_FEATURES_ENABLE_FUNCTIONS"}
	if !env.Cfg.Client.Features.EnableFunctions {
		return Result{}.Skip("CLIENT_FEATURES_ENABLE_FUNCTIONS=false，Worker 功能未启用")
	}

	binPath := env.Cfg.Client.Worker.WorkerdBinaryPath
	if binPath == "" {
		return Result{}.Warn("未找到 workerd 可执行文件（conf.Complete 自动探测失败）；Functions 功能将不可用").ConfigRefs(refs...)
	}
	if st, err := os.Stat(binPath); err != nil {
		return Result{}.Fail(fmt.Sprintf("workerd 二进制不存在: %s", binPath)).ConfigRefs(refs...)
	} else if st.IsDir() {
		return Result{}.Fail(fmt.Sprintf("workerd 路径指向目录而非可执行文件: %s", binPath)).ConfigRefs(refs...)
	}

	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, binPath, "--version").CombinedOutput()
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("workerd --version 执行失败: %v", err)).
			WithDetails(truncate(string(out), 200)).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("workerd 可执行: %s", binPath)).
		WithDetails(truncate(string(out), 120)).ConfigRefs(refs...)
}

// ---------------- worker/workdir_files ----------------

type workerWorkdirFiles struct{ baseChecker }

func (c *workerWorkdirFiles) Run(ctx context.Context, env *Env) Result {
	refs := []string{"CLIENT_WORKER_WORKERD_WORK_DIR", "services/workerd/file.go"}
	rows, err := loadWorkers(ctx, env)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if len(rows) == 0 {
		return Result{}.Skip("workers 表为空")
	}

	workDir := env.Cfg.Client.Worker.WorkerdWorkDir
	var details, problems []string
	checked := 0
	for _, w := range rows {
		if !env.NodeFilter(w.ID) {
			continue
		}
		if w.CodeEntry == "" {
			continue
		}
		codeFile := filepath.Join(workDir, defs.WorkerInfoPath, w.ID, defs.WorkerCodePath, w.CodeEntry)
		data, err := os.ReadFile(codeFile)
		if err != nil {
			// 未部署过属正常（面板 create 后才落盘）
			details = append(details, fmt.Sprintf("worker=%s — 磁盘文件缺失（未部署或已清理）: %s", w.ID, codeFile))
			continue
		}
		checked++
		if utils.MD5(data) != utils.MD5([]byte(w.Code)) {
			problems = append(problems, fmt.Sprintf("worker=%s — 磁盘代码与数据库不一致（建议 redeploy）", w.ID))
		} else {
			details = append(details, fmt.Sprintf("worker=%s — 代码一致", w.ID))
		}
	}
	if len(problems) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d 个 Worker 代码漂移", len(problems))).WithDetails(append(problems, details...)...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("Worker 代码文件检查完成（比对 %d 个）", checked)).WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- worker/ingress_refs ----------------

type workerIngressRefs struct{ baseChecker }

func (c *workerIngressRefs) Run(ctx context.Context, env *Env) Result {
	refs := []string{"proxy_config.worker_id", "defs.FrpProxyAnnotationsKey_WorkerId"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	rows := []models.ProxyConfig{}
	if err := db.Model(&models.ProxyConfig{}).Where("deleted_at IS NULL AND worker_id != ''").Find(&rows).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 proxy_config 失败: %v", err))
	}
	if len(rows) == 0 {
		return Result{}.Skip("没有引用 worker 的隧道（ingress 未启用）")
	}

	var broken []string
	for _, r := range rows {
		if !env.NodeFilter(r.ClientID, r.ServerID) {
			continue
		}
		var cnt int64
		if err := db.Model(&models.Worker{}).Where("id = ?", r.WorkerID).Count(&cnt).Error; err != nil || cnt == 0 {
			broken = append(broken, fmt.Sprintf("proxy=%s 引用 worker_id=%s 不存在", r.Name, r.WorkerID))
		}
	}
	if len(broken) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 条隧道引用了不存在的 Worker", len(broken))).WithDetails(broken...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 条 worker 隧道引用完整", len(rows))).ConfigRefs(refs...)
}
