package checker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"gorm.io/gorm"
)

// fix.go `--fix` 一键修复。
//
// 三条内置修复动作，默认只列出（dry-run），--destructive 才实际执行：
//  1. orphan_proxy_configs —— 软删除引用断裂的隧道记录（可在删除审计中找回）
//  2. sort_index_duplicates —— 归一化同租户下重复的排序序号（按现有顺序重排为 1..N）
//  3. deprecated_env_vars —— 生成废弃环境变量的清理命令清单（仅输出，不产生写操作）
//
// 修复使用 --fix 显式传入的读写数据库句柄（env.DB 为只读连接，不在此使用）。

// FixStatus 修复动作的执行状态。
const (
	FixStatusNone    = "none"    // 无需处理
	FixStatusListed  = "listed"  // dry-run：已列出将执行的动作
	FixStatusApplied = "applied" // 已实际执行
	FixStatusFailed  = "failed"  // 执行失败
)

// FixOutcome 一条修复动作的结果。
type FixOutcome struct {
	Fixer  string   `json:"fixer"`
	Status string   `json:"status"`
	Detail []string `json:"detail,omitempty"`
}

// ApplyFixes 依次执行全部修复器；dryRun=true 时仅列出将执行的动作。
func ApplyFixes(ctx context.Context, db *gorm.DB, env *Env, dryRun bool) []FixOutcome {
	outcomes := fixOrphanProxyConfigs(db, dryRun)
	outcomes = append(outcomes, fixSortIndexDuplicates(db, dryRun)...)
	outcomes = append(outcomes, fixDeprecatedEnvVars(env)...)
	return outcomes
}

// RenderFixText 输出修复结果的终端文本。
func RenderFixText(outcomes []FixOutcome) string {
	var b strings.Builder
	b.WriteString("\n一键修复结果:\n")
	for _, o := range outcomes {
		status := map[string]string{
			FixStatusNone:    "无需处理",
			FixStatusListed:  "将执行（--destructive 实际执行）",
			FixStatusApplied: "已执行",
			FixStatusFailed:  "执行失败",
		}[o.Status]
		fmt.Fprintf(&b, "  [%s] %s — %s\n", o.Fixer, status, strings.Join(o.Detail, "; "))
	}
	return b.String()
}

// fixOrphanProxyConfigs 清理 client/server 引用断裂的隧道记录（软删除）。
func fixOrphanProxyConfigs(db *gorm.DB, dryRun bool) []FixOutcome {
	outcome := FixOutcome{Fixer: "orphan_proxy_configs", Status: FixStatusNone}

	rows := []models.ProxyConfig{}
	if err := db.Model(&models.ProxyConfig{}).Find(&rows).Error; err != nil {
		outcome.Status = FixStatusFailed
		outcome.Detail = append(outcome.Detail, fmt.Sprintf("读取 proxy_config 失败: %v", err))
		return []FixOutcome{outcome}
	}
	if len(rows) == 0 {
		outcome.Detail = append(outcome.Detail, "proxy_config 为空")
		return []FixOutcome{outcome}
	}

	var orphanIDs []uint
	clientExists := map[string]bool{}
	serverExists := map[string]bool{}
	for _, r := range rows {
		if r.ClientID != "" {
			var cnt int64
			if db.Model(&models.Client{}).Unscoped().Where("client_id = ?", r.ClientID).Count(&cnt).Error == nil {
				clientExists[r.ClientID] = cnt > 0
			}
		}
		if r.ServerID != "" {
			var cnt int64
			if db.Model(&models.Server{}).Unscoped().Where("server_id = ?", r.ServerID).Count(&cnt).Error == nil {
				serverExists[r.ServerID] = cnt > 0
			}
		}
	}
	for i := range rows {
		r := &rows[i]
		clientMissing := r.ClientID != "" && !clientExists[r.ClientID]
		serverMissing := r.ServerID != "" && !serverExists[r.ServerID]
		if clientMissing || serverMissing {
			orphanIDs = append(orphanIDs, r.ID)
		}
	}
	if len(orphanIDs) == 0 {
		outcome.Detail = append(outcome.Detail, fmt.Sprintf("%d 条隧道记录引用完整", len(rows)))
		return []FixOutcome{outcome}
	}

	outcome.Status = FixStatusListed
	outcome.Detail = append(outcome.Detail, fmt.Sprintf("发现 %d 条孤儿隧道记录", len(orphanIDs)))
	if dryRun {
		outcome.Detail = append(outcome.Detail, fmt.Sprintf("将软删除 ids=%v（可在删除审计找回）", orphanIDs))
		return []FixOutcome{outcome}
	}
	if err := db.Delete(&models.ProxyConfig{}, orphanIDs).Error; err != nil {
		outcome.Status = FixStatusFailed
		outcome.Detail = append(outcome.Detail, fmt.Sprintf("删除失败: %v", err))
		return []FixOutcome{outcome}
	}
	outcome.Status = FixStatusApplied
	outcome.Detail = append(outcome.Detail, fmt.Sprintf("已软删除 %d 条孤儿记录", len(orphanIDs)))
	return []FixOutcome{outcome}
}

// fixSortIndexDuplicates 归一化同租户下重复的非零 sort_index：
// 将该组内非零序号的记录按（原序号, 创建时间）重排为 1..N；0 表示未设置，保持不动。
func fixSortIndexDuplicates(db *gorm.DB, dryRun bool) []FixOutcome {
	outcomes := []FixOutcome{}
	outcomes = append(outcomes, normalizeSortIndex(db, dryRun, &models.Client{})...)
	outcomes = append(outcomes, normalizeSortIndex(db, dryRun, &models.Server{})...)
	return outcomes
}

func normalizeSortIndex(db *gorm.DB, dryRun bool, model interface{}) []FixOutcome {
	var outcomes []FixOutcome
	table := ""
	switch model.(type) {
	case *models.Client:
		table = "clients"
	case *models.Server:
		table = "servers"
	}

	type row struct {
		key      interface{} // 主键（client_id / server_id）
		userID   int
		tenantID int
		sort     int
		created  time.Time
	}
	var rows []row
	keyColumn := ""

	switch m := model.(type) {
	case *models.Client:
		keyColumn = "client_id"
		var cs []models.Client
		if err := db.Model(m).Where("deleted_at IS NULL").
			Order("sort_index ASC, created_at ASC").Find(&cs).Error; err != nil {
			return []FixOutcome{{Fixer: "sort_index_duplicates", Status: FixStatusFailed,
				Detail: []string{fmt.Sprintf("读取 %s 失败: %v", table, err)}}}
		}
		for _, c := range cs {
			if c.ClientEntity == nil {
				continue
			}
			rows = append(rows, row{key: c.ClientID, userID: c.UserID, tenantID: c.TenantID, sort: c.SortIndex, created: c.CreatedAt})
		}
	case *models.Server:
		keyColumn = "server_id"
		var ss []models.Server
		if err := db.Model(m).Where("deleted_at IS NULL").
			Order("sort_index ASC, created_at ASC").Find(&ss).Error; err != nil {
			return []FixOutcome{{Fixer: "sort_index_duplicates", Status: FixStatusFailed,
				Detail: []string{fmt.Sprintf("读取 %s 失败: %v", table, err)}}}
		}
		for _, s := range ss {
			if s.ServerEntity == nil {
				continue
			}
			rows = append(rows, row{key: s.ServerID, userID: s.UserID, tenantID: s.TenantID, sort: s.SortIndex, created: s.CreatedAt})
		}
	}

	groups := map[[2]int][]row{}
	for _, r := range rows {
		groups[[2]int{r.userID, r.tenantID}] = append(groups[[2]int{r.userID, r.tenantID}], r)
	}

	for g, groupRows := range groups {
		// 组内非零序号存在重复才需要归一化
		seen := map[int]bool{}
		dup := false
		var nonzero []row
		for _, r := range groupRows {
			if r.sort != 0 {
				if seen[r.sort] {
					dup = true
				}
				seen[r.sort] = true
				nonzero = append(nonzero, r)
			}
		}
		if !dup {
			continue
		}
		outcome := FixOutcome{Fixer: "sort_index_duplicates"}
		label := fmt.Sprintf("%s 组(user=%d, tenant=%d): %d 行重复序号", table, g[0], g[1], len(nonzero))
		if dryRun {
			outcome.Status = FixStatusListed
			outcome.Detail = append(outcome.Detail, label+"，将按现有顺序重排为 1..N")
			outcomes = append(outcomes, outcome)
			continue
		}
		applied := 0
		for i, r := range nonzero {
			if err := db.Model(model).Where(keyColumn+" = ? AND sort_index = ?", r.key, r.sort).
				Update("sort_index", i+1).Error; err != nil {
				outcome.Status = FixStatusFailed
				outcome.Detail = append(outcome.Detail, fmt.Sprintf("%s 更新失败: %v", label, err))
				outcomes = append(outcomes, outcome)
				continue
			}
			applied++
		}
		if outcome.Status != FixStatusFailed {
			outcome.Status = FixStatusApplied
			outcome.Detail = append(outcome.Detail, fmt.Sprintf("%s，已重排 %d 行", label, applied))
		}
		outcomes = append(outcomes, outcome)
	}
	if len(outcomes) == 0 {
		outcomes = append(outcomes, FixOutcome{Fixer: "sort_index_duplicates", Status: FixStatusNone,
			Detail: []string{"未发现重复的非零排序序号"}})
	}
	return outcomes
}

// fixDeprecatedEnvVars 输出废弃环境变量的清理命令清单（信息性动作，永不产生写操作）。
func fixDeprecatedEnvVars(env *Env) []FixOutcome {
	outcome := FixOutcome{Fixer: "deprecated_env_vars"}
	var commands []string
	for _, d := range deprecatedEnvVars {
		if _, ok := os.LookupEnv(d.name); ok {
			commands = append(commands, "unset "+d.name)
		}
	}
	if len(commands) == 0 {
		outcome.Status = FixStatusNone
		outcome.Detail = append(outcome.Detail, "未发现废弃环境变量")
		return []FixOutcome{outcome}
	}
	outcome.Status = FixStatusListed
	outcome.Detail = append(outcome.Detail,
		"以下命令可清理无效环境变量（需自行从 .env / 服务环境同步移除）:")
	outcome.Detail = append(outcome.Detail, commands...)
	return []FixOutcome{outcome}
}
