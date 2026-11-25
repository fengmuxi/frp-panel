package workerd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

type workersManager struct {
	workers *utils.SyncMap[string, app.WorkerController]
}

func NewWorkersManager() *workersManager {
	return &workersManager{
		workers: &utils.SyncMap[string, app.WorkerController]{},
	}
}

func (m *workersManager) GetWorker(ctx *app.Context, id string) (app.WorkerController, bool) {
	return m.workers.Load(id)
}

func (m *workersManager) RunWorker(ctx *app.Context, id string, worker app.WorkerController) error {
	if !ctx.GetApp().GetConfig().Client.Features.EnableFunctions {
		logger.Logger(ctx).Errorf("function features are not enabled")
		return fmt.Errorf("function features are not enabled")
	}

	worker.RunWorker(ctx)

	m.workers.Store(id, worker)
	return nil
}

func (m *workersManager) StopWorker(ctx *app.Context, id string) error {
	worker, ok := m.workers.Load(id)
	if !ok {
		return fmt.Errorf("cannot find worker, id: %s", id)
	}
	worker.StopWorker(ctx)
	m.workers.Delete(id)
	return nil
}

func (m *workersManager) StopAllWorkers(ctx *app.Context) {
	m.workers.Range(func(k string, v app.WorkerController) bool {
		v.StopWorker(ctx)
		return true
	})

	tmpM := m.workers.ToMap()
	for k := range tmpM {
		m.workers.Delete(k)
	}
}

func (m *workersManager) GetWorkerStatus(ctx *app.Context, id string) (defs.WorkerStatus, error) {
	ok, err := utils.ProcessExistsBySelf(id)
	if err != nil {
		return defs.WorkerStatus_Unknown, err
	}
	if ok {
		return defs.WorkerStatus_Running, nil
	}
	return defs.WorkerStatus_Inactive, nil
}

func (m *workersManager) InstallWorkerd(ctx *app.Context, url string, installDir string) (string, error) {
	arch := runtime.GOARCH
	goos := runtime.GOOS

	workerDownloadCfg := ctx.GetApp().GetConfig().Client.Worker.WorkerdDownloadURL

	if goos != "linux" {
		return "", fmt.Errorf("unsupported os: %s", goos)
	}
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported arch: %s", arch)
	}

	// 安全审计 P1-12：请求中的下载地址覆盖曾被原样使用（任意 URL → chmod 0755 → root 执行），
	// 现在一律使用节点本地配置（WORKERD_DOWNLOAD_URL_*）并强制 https
	downloadUrl := ""
	switch arch {
	case "amd64":
		downloadUrl = workerDownloadCfg.LinuxX8664
	case "arm64":
		downloadUrl = workerDownloadCfg.LinuxArm64
	default:
		return "", fmt.Errorf("unsupported arch: %s", arch)
	}
	if len(url) > 0 && url != downloadUrl {
		logger.Logger(ctx).Warnf("ignore workerd download url override from request: [%s]", url)
	}
	if !strings.HasPrefix(downloadUrl, "https://") {
		return "", fmt.Errorf("workerd download url must be https, got [%s]", downloadUrl)
	}

	if workerDownloadCfg.UseProxy {
		if len(ctx.GetApp().GetConfig().App.GithubProxyUrl) > 0 {
			downloadUrl = fmt.Sprintf("%s/%s", ctx.GetApp().GetConfig().App.GithubProxyUrl, downloadUrl)
		}
	}

	proxyUrl := ctx.GetApp().GetConfig().HTTP_PROXY

	path, err := utils.DownloadFile(ctx, downloadUrl, proxyUrl)
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("failed to download workerd, url: %s", downloadUrl)
		return "", err
	}
	// 下载产物（.gz）提取完成后即无用：清理临时目录，避免 /tmp/downloads 无限增长
	defer os.RemoveAll(path)

	if len(installDir) == 0 {
		installDir = "/usr/local/bin"
	}

	// TOFU 指纹（安全审计 P1-12 纵深）：首次安装记录产物 SHA-256，后续安装必须一致；
	// 防止首次安装之后的下载被替换（首次依赖 https 本身）
	if err := pinWorkerdChecksum(ctx, path, installDir); err != nil {
		return "", err
	}

	finalPath, err := utils.ExtractGZTo(path, "workerd", installDir)
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("failed to extract workerd, path: %s", path)
		return "", err
	}

	logger.Logger(ctx).Infof("workerd installed successfully, path: %s", finalPath)

	return finalPath, nil
}

// pinWorkerdChecksum TOFU 指纹校验（安全审计 P1-12 纵深）：
// 首次安装把下载产物的 SHA-256 记录到 installDir/workerd.sha256.pin，
// 后续安装的产物哈希必须与记录一致，否则拒绝安装（防下载被替换投毒；
// 首次安装依赖 https 与节点本地配置的官方源）。
func pinWorkerdChecksum(ctx *app.Context, downloadedPath, installDir string) error {
	raw, err := os.ReadFile(downloadedPath)
	if err != nil {
		return fmt.Errorf("read downloaded workerd: %w", err)
	}
	sum := sha256.Sum256(raw)
	hexSum := hex.EncodeToString(sum[:])

	pinPath := filepath.Join(installDir, "workerd.sha256.pin")
	if existing, err := os.ReadFile(pinPath); err == nil {
		if strings.TrimSpace(string(existing)) != hexSum {
			// 升级路径：确认要升级 workerd 时，删除 installDir 下的
			// workerd.sha256.pin 文件后重新安装（重新 TOFU 固化新哈希）
			return fmt.Errorf("workerd checksum mismatch: pinned=%s actual=%s, refusing install (to upgrade, delete the pin file and retry)",
				strings.TrimSpace(string(existing)), hexSum)
		}
		return nil
	}
	if err := os.WriteFile(pinPath, []byte(hexSum), 0600); err != nil {
		// 指纹写失败只影响后续去重，首次安装继续
		logger.Logger(ctx).Warnf("cannot write workerd checksum pin [%s]: %v", pinPath, err)
	}
	return nil
}
