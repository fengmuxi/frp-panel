package checker

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fengmuxi/frp-panel/biz/common/upgrade"
)

func buildWebUIGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&webuiIndex{baseChecker{name: "index_page", group: "webui", severity: LevelCritical,
			fixHint: "重建前端（build.sh 内 pnpm build）或改用带 embed 产物的官方发布二进制；GET / 404 说明静态资源缺失"}},
		&webuiStaticAssets{baseChecker{name: "static_assets", group: "webui", severity: LevelMajor,
			fixHint: "静态资源 404 通常是 OTA 后前端产物与二进制错位；重新构建并整体替换二进制"}},
		&webuiLogStream{baseChecker{name: "log_stream", group: "webui", severity: LevelMajor,
			fixHint: "/api/v1/log 是浏览器日志流入口，匿名必须 401；200/404 说明 streamlog 路由异常"}},
		&webuiPbSync{baseChecker{name: "pb_sync", group: "webui", severity: LevelMinor,
			fixHint: "www/lib/pb 与 idl/ 的修改时间漂移大时，前端 protobuf-ts 代码可能落后于后端，重新执行 codegen.sh"}},
		&webuiVersionMatch{baseChecker{name: "version_match", group: "webui", severity: LevelMinor,
			fixHint: "OTA 升级后需整体替换二进制（前端产物 embed 在内）；单独替换会造成前后端版本错位"}},
	}, nil
}

// ---------------- webui/index_page ----------------

type webuiIndex struct{ baseChecker }

func (c *webuiIndex) Run(ctx context.Context, env *Env) Result {
	refs := []string{"GET /（Next.js 导出产物 embed 于 cmd/frpp/out）"}
	code, hdr, body, err := env.Get(ctx, "/")
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("访问 WebUI 首页失败: %v", err)).ConfigRefs(refs...)
	}
	if code != http.StatusOK {
		return Result{}.Fail(fmt.Sprintf("WebUI 首页返回 http=%d", code)).ConfigRefs(refs...)
	}
	if !strings.Contains(hdr.Get("Content-Type"), "html") {
		return Result{}.Warn(fmt.Sprintf("首页 Content-Type=%s 非 HTML", hdr.Get("Content-Type"))).ConfigRefs(refs...)
	}
	return Result{}.Pass("WebUI 首页可访问").
		WithDetails(fmt.Sprintf("size=%d bytes", len(body))).ConfigRefs(refs...)
}

// ---------------- webui/static_assets ----------------

type webuiStaticAssets struct{ baseChecker }

// nextAssetRegex 从 Next.js HTML 中提取静态资源引用。
var nextAssetRegex = regexp.MustCompile(`(?:src|href)="(/_next/[^"]+)"`)

func (c *webuiStaticAssets) Run(ctx context.Context, env *Env) Result {
	refs := []string{"GET /_next/*"}
	_, _, body, err := env.Get(ctx, "/")
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("首页不可达，跳过静态资源检查: %v", err)).ConfigRefs(refs...)
	}

	matches := nextAssetRegex.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return Result{}.Skip("首页未引用 _next 静态资源（可能是登录跳转页）").ConfigRefs(refs...)
	}

	seen := map[string]bool{}
	var broken []string
	var okCount int
	for _, m := range matches {
		path := m[1]
		if seen[path] || len(seen) >= 5 { // 最多抽测 5 个资源，避免拖慢整体
			continue
		}
		seen[path] = true
		code, _, _, err := env.Get(ctx, path)
		switch {
		case err != nil:
			broken = append(broken, fmt.Sprintf("%s — 请求失败: %v", path, err))
		case code != http.StatusOK:
			broken = append(broken, fmt.Sprintf("%s — http=%d", path, code))
		default:
			okCount++
		}
	}
	if len(broken) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d/%d 个静态资源加载失败（OTA 后产物错位的典型症状）", len(broken), okCount+len(broken))).
			WithDetails(broken...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("抽测 %d 个静态资源全部可加载", okCount)).ConfigRefs(refs...)
}

// ---------------- webui/log_stream ----------------

type webuiLogStream struct{ baseChecker }

func (c *webuiLogStream) Run(ctx context.Context, env *Env) Result {
	refs := []string{"GET /api/v1/log", "biz/master/streamlog"}
	code, _, _, err := env.Get(ctx, "/api/v1/log")
	if err != nil {
		return Result{}.Fail(fmt.Sprintf("日志流端点不可达: %v", err)).ConfigRefs(refs...)
	}
	if code == http.StatusUnauthorized {
		return Result{}.Pass("匿名访问日志流被正确拒绝(401)").ConfigRefs(refs...)
	}
	return Result{}.Fail(fmt.Sprintf("匿名访问 /api/v1/log 返回 http=%d（期望 401）", code)).ConfigRefs(refs...)
}

// ---------------- webui/pb_sync ----------------

type webuiPbSync struct{ baseChecker }

func (c *webuiPbSync) Run(ctx context.Context, env *Env) Result {
	refs := []string{"www/lib/pb/", "idl/*.proto", "codegen.sh"}
	pbDir := filepath.Join("www", "lib", "pb")
	idlDir := "idl"

	latestPB, err1 := latestModTime(pbDir)
	latestIDL, err2 := latestModTime(idlDir)
	if err1 != nil || err2 != nil {
		return Result{}.Skip(fmt.Sprintf("仓库目录不可见（非源码目录运行）: pb=%v idl=%v", err1, err2)).ConfigRefs(refs...)
	}

	drift := latestIDL.Sub(latestPB)
	if drift > 0 {
		if drift > 24*time.Hour {
			return Result{}.Warn(fmt.Sprintf("idl/ 最新修改比 www/lib/pb 晚 %s，前端 protobuf-ts 生成代码可能过期，请执行 codegen.sh", drift.Truncate(time.Hour))).
				WithDetails(fmt.Sprintf("idl=%s pb=%s", latestIDL.Format(time.RFC3339), latestPB.Format(time.RFC3339))).ConfigRefs(refs...)
		}
		return Result{}.Pass(fmt.Sprintf("idl 领先前端生成代码 %s（新近变更，注意同步）", drift.Truncate(time.Minute))).ConfigRefs(refs...)
	}
	return Result{}.Pass("www/lib/pb 与 idl/ 无明显漂移").
		WithDetails(fmt.Sprintf("idl=%s pb=%s", latestIDL.Format(time.RFC3339), latestPB.Format(time.RFC3339))).ConfigRefs(refs...)
}

func latestModTime(dir string) (time.Time, error) {
	var latest time.Time
	entries, err := os.ReadDir(dir)
	if err != nil {
		return latest, err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	if latest.IsZero() {
		return latest, fmt.Errorf("empty dir %s", dir)
	}
	return latest, nil
}

// ---------------- webui/version_match ----------------

type webuiVersionMatch struct{ baseChecker }

func (c *webuiVersionMatch) Run(ctx context.Context, env *Env) Result {
	refs := []string{"conf.GetVersion()", "frpp upgrade status.json", "common.Version(由 build.sh ldflags 注入)"}
	st, path, err := upgrade.ReadStatus("")
	if err != nil {
		return Result{}.Pass("无 OTA 记录，前后端版本随二进制一体发布").ConfigRefs(refs...)
	}

	details := []string{
		fmt.Sprintf("binary version=%s build=%s", env.Version.GitVersion, env.Version.BuildDate),
		fmt.Sprintf("upgrade status=%s (%s)", path, st.UpdatedAt.Format(time.RFC3339)),
	}

	// 状态文件时间与二进制构建时间差异过大 → 提示可能的错位
	buildDate, _ := time.Parse(time.RFC3339, env.Version.BuildDate)
	if !buildDate.IsZero() && st.UpdatedAt.Sub(buildDate) > 30*24*time.Hour {
		return Result{}.Warn("OTA 记录与二进制构建时间相差超过 30 天，疑似手动替换过二进制，前后端版本可能错位").
			WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass("版本一致性无明显异常").WithDetails(details...).ConfigRefs(refs...)
}
