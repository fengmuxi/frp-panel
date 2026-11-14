package checker

import (
	"context"
	"fmt"
	"strings"

	"github.com/VaalaCat/frp-panel/models"
)

func buildDataGroup(env *Env) ([]Checker, error) {
	return []Checker{
		&dataOrphanProxies{baseChecker{name: "orphan_proxies", group: "data", severity: LevelMajor,
			fixHint: "删除节点应级联清理隧道；孤儿 proxy_config 会在统计页产生幽灵记录，可手动删除或保留排查"}},
		&dataOrphanWorkers{baseChecker{name: "orphan_workers", group: "data", severity: LevelMinor,
			fixHint: "worker_clients 关联断裂的 Worker 无法被任何节点拉取，重新分配到节点或删除"}},
		&dataOrphanWG{baseChecker{name: "orphan_wg", group: "data", severity: LevelMajor,
			fixHint: "WireGuard/Endpoint/Link 引用断裂会导致拓扑计算失败；清理断链记录后重建"}},
		&dataSoftDeleted{baseChecker{name: "soft_deleted", group: "data", severity: LevelInfo,
			fixHint: "GORM 软删除记录仅供审计，数量过大时考虑归档清理以控制库体积"}},
		&dataStatsReconcile{baseChecker{name: "stats_reconcile", group: "data", severity: LevelMinor,
			fixHint: "history_proxy_stats 是每日 CollectDailyStats 快照（0 0 3 * * *），与 proxy_stats 的差异应等于当日增量"}},
		&dataMigrationVersion{baseChecker{name: "db_migration", group: "data", severity: LevelMinor,
			fixHint: "迁移落后于代码说明 master 未用当前版本启动过或迁移中断；用当前版本 frpp 启动一次 master 即可应用未跑完的迁移"}},
		&dataSchemaDiff{baseChecker{name: "schema_diff", group: "data", severity: LevelMajor,
			fixHint: "模型与库表不一致说明版本回退或迁移中断；用当前版本 frpp 启动一次 master 会触发 AutoMigrate 修复"}},
	}, nil
}

// ---------------- data/orphan_proxies ----------------

type dataOrphanProxies struct{ baseChecker }

func (c *dataOrphanProxies) Run(ctx context.Context, env *Env) Result {
	refs := []string{"proxy_config.client_id/server_id", "clients/servers 表", "删除节点级联清理"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	rows := []models.ProxyConfig{}
	if err := db.Model(&models.ProxyConfig{}).Find(&rows).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 proxy_config 失败: %v", err))
	}
	if len(rows) == 0 {
		return Result{}.Skip("proxy_config 表为空")
	}

	var orphans []string
	for _, r := range rows {
		if !env.NodeFilter(r.ClientID, r.ServerID) {
			continue
		}
		var cnt int64
		if r.ClientID != "" {
			if err := db.Model(&models.Client{}).Unscoped().Where("client_id = ?", r.ClientID).Count(&cnt).Error; err == nil && cnt == 0 {
				orphans = append(orphans, fmt.Sprintf("proxy=%s 引用的 client=%s 已不存在", r.Name, r.ClientID))
			}
		}
		if r.ServerID != "" {
			if err := db.Model(&models.Server{}).Unscoped().Where("server_id = ?", r.ServerID).Count(&cnt).Error; err == nil && cnt == 0 {
				orphans = append(orphans, fmt.Sprintf("proxy=%s 引用的 server=%s 已不存在", r.Name, r.ServerID))
			}
		}
	}
	if len(orphans) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 条孤儿隧道记录", len(orphans))).WithDetails(orphans...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 条隧道记录引用完整", len(rows))).ConfigRefs(refs...)
}

// ---------------- data/orphan_workers ----------------

type dataOrphanWorkers struct{ baseChecker }

func (c *dataOrphanWorkers) Run(ctx context.Context, env *Env) Result {
	refs := []string{"worker_clients(many2many)", "workers 表"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	// 通过 GORM 关联预载统计被 client 引用的 Worker（不依赖连接表列名细节）
	rows := []models.Worker{}
	if err := db.Model(&models.Worker{}).Find(&rows).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 workers 失败: %v", err))
	}
	if len(rows) == 0 {
		return Result{}.Skip("workers 表为空")
	}

	var clients []models.Client
	if err := db.Model(&models.Client{}).Preload("Workers").Find(&clients).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("预载 client workers 失败: %v", err))
	}

	referenced := map[string]bool{}
	for _, cli := range clients {
		for _, w := range cli.Workers {
			if w.WorkerEntity != nil {
				referenced[w.WorkerEntity.ID] = true
			}
		}
	}

	var orphans []string
	for _, w := range rows {
		if !referenced[w.ID] {
			orphans = append(orphans, w.ID)
		}
	}
	if len(orphans) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d/%d 个 Worker 未挂载到任何 client", len(orphans), len(rows))).
			WithDetails(joinLimit(orphans, 10)).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 个 Worker 关联关系完整", len(rows))).ConfigRefs(refs...)
}

// ---------------- data/orphan_wg ----------------

type dataOrphanWG struct{ baseChecker }

func (c *dataOrphanWG) Run(ctx context.Context, env *Env) Result {
	refs := []string{"wireguards.client_id", "endpoints.wireguard_id", "wireguard_links 两端", "删除节点级联清理"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	var problems []string

	// wireguards -> clients
	wgs := []models.WireGuard{}
	if err := db.Model(&models.WireGuard{}).Find(&wgs).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 wireguards 失败: %v", err))
	}
	for _, w := range wgs {
		var cnt int64
		if err := db.Model(&models.Client{}).Unscoped().Where("client_id = ?", w.ClientID).Count(&cnt).Error; err == nil && cnt == 0 {
			problems = append(problems, fmt.Sprintf("wireguard=%s 引用的 client=%s 已不存在", w.Name, w.ClientID))
		}
	}

	// endpoints -> wireguards
	eps := []models.Endpoint{}
	if err := db.Model(&models.Endpoint{}).Find(&eps).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 endpoints 失败: %v", err))
	}
	for _, e := range eps {
		if e.WireGuardID == 0 {
			continue
		}
		var cnt int64
		if err := db.Model(&models.WireGuard{}).Where("id = ?", e.WireGuardID).Count(&cnt).Error; err == nil && cnt == 0 {
			problems = append(problems, fmt.Sprintf("endpoint=%s:%d 引用的 wireguard_id=%d 已不存在", e.Host, e.Port, e.WireGuardID))
		}
	}

	if len(problems) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 处组网引用断裂", len(problems))).WithDetails(problems...).ConfigRefs(refs...)
	}
	return Result{}.Pass("组网数据引用完整").WithDetails(fmt.Sprintf("wireguards=%d endpoints=%d", len(wgs), len(eps))).ConfigRefs(refs...)
}

// ---------------- data/soft_deleted ----------------

type dataSoftDeleted struct{ baseChecker }

func (c *dataSoftDeleted) Run(ctx context.Context, env *Env) Result {
	refs := []string{"gorm.DeletedAt（软删除）", "删除节点级联清理审计"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	var details []string
	tables := []struct {
		name  string
		model interface{}
	}{
		{"clients", &models.Client{}},
		{"servers", &models.Server{}},
		{"proxy_config", &models.ProxyConfig{}},
		{"proxy_stats", &models.ProxyStats{}},
		{"workers", &models.Worker{}},
		{"wireguards", &models.WireGuard{}},
	}
	total := int64(0)
	for _, t := range tables {
		var cnt int64
		if err := db.Model(t.model).Unscoped().Where("deleted_at IS NOT NULL").Count(&cnt).Error; err == nil && cnt > 0 {
			details = append(details, fmt.Sprintf("%s: %d 条软删除记录", t.name, cnt))
			total += cnt
		}
	}
	if total == 0 {
		return Result{}.Pass("无软删除残留").ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("共 %d 条软删除记录（审计用）", total)).WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- data/stats_reconcile ----------------

type dataStatsReconcile struct{ baseChecker }

func (c *dataStatsReconcile) Run(ctx context.Context, env *Env) Result {
	refs := []string{"proxy_stats", "history_proxy_stats", "proxy.CollectDailyStats"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	var todayTotal, historyTotal int64
	if err := db.Model(&models.ProxyStats{}).
		Select("COALESCE(SUM(today_traffic_in + today_traffic_out),0)").Row().Scan(&todayTotal); err != nil {
		return Result{}.Skip(fmt.Sprintf("聚合 proxy_stats 失败: %v", err))
	}
	if err := db.Model(&models.HistoryProxyStats{}).
		Select("COALESCE(SUM(traffic_in + traffic_out),0)").Row().Scan(&historyTotal); err != nil {
		return Result{}.Skip(fmt.Sprintf("聚合 history_proxy_stats 失败: %v", err))
	}

	details := []string{
		fmt.Sprintf("今日累计(today in+out): %d bytes", todayTotal),
		fmt.Sprintf("历史快照累计(history in+out): %d bytes", historyTotal),
	}
	if todayTotal < 0 || historyTotal < 0 {
		return Result{}.Fail("流量聚合出现负数（溢出）").WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass("今日/累计统计可对账").WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- data/schema_diff ----------------

type dataSchemaDiff struct{ baseChecker }

func (c *dataSchemaDiff) Run(ctx context.Context, env *Env) Result {
	refs := []string{"models/db.go AutoMigrate 清单", "gorm Migrator().HasColumn"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	// 库未初始化（表都不存在）时跳过 —— 该场景由 env/db_connect 报告
	if !db.Migrator().HasTable(&models.Client{}) {
		return Result{}.Skip("clients 表不存在（库尚未初始化），schema 对账跳过")
	}

	// 关键字段抽查（表存在性已在 env/db_connect 检查）
	cases := []struct {
		model  interface{}
		column string
		label  string
	}{
		{&models.Client{}, "connect_secret", "clients.connect_secret"},
		{&models.Client{}, "ephemeral", "clients.ephemeral"},
		{&models.Client{}, "sort_index", "clients.sort_index"},
		{&models.Server{}, "frps_urls", "servers.frps_urls"},
		{&models.ProxyConfig{}, "quota_limit", "proxy_config.quota_limit"},
		{&models.ProxyConfig{}, "schedule_enabled", "proxy_config.schedule_enabled"},
		{&models.Worker{}, "config_template", "workers.config_template"},
		{&models.WireGuard{}, "ws_listen_port", "wireguards.ws_listen_port"},
		{&models.User{}, "tenant_id", "users.tenant_id"},
	}

	var missing []string
	for _, cs := range cases {
		if !db.Migrator().HasColumn(cs.model, cs.column) {
			missing = append(missing, cs.label)
		}
	}
	if len(missing) > 0 {
		return Result{}.Fail(fmt.Sprintf("缺少 %d 个模型字段（schema 落后于代码）: %s", len(missing), strings.Join(missing, ", "))).ConfigRefs(refs...)
	}
	return Result{}.Pass("关键模型字段与库表一致（抽查 9 项）").ConfigRefs(refs...)
}

// ---------------- data/db_migration ----------------

type dataMigrationVersion struct{ baseChecker }

func (c *dataMigrationVersion) Run(ctx context.Context, env *Env) Result {
	refs := []string{"models/migrate.go", "schema_migrations 表"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	latest := models.LatestMigrationVersion()
	if !db.Migrator().HasTable(&models.MigrationRecord{}) {
		return Result{}.Warn(fmt.Sprintf("schema_migrations 表不存在（库由不支持迁移的旧版本创建），当前代码迁移版本: %d", latest)).
			ConfigRefs(refs...)
	}

	var applied []models.MigrationRecord
	if err := db.Model(&models.MigrationRecord{}).Order("version DESC").Limit(1).Find(&applied).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 schema_migrations 失败: %v", err))
	}
	current := int64(0)
	if len(applied) > 0 {
		current = applied[0].Version
	}
	details := []string{fmt.Sprintf("库内迁移版本: %d, 代码最新迁移版本: %d", current, latest)}

	switch {
	case current >= latest:
		return Result{}.Pass("版本化迁移已全部应用").WithDetails(details...).ConfigRefs(refs...)
	case current > latest:
		return Result{}.Warn("库内迁移版本高于代码（二进制被回退，后续启动不会再执行旧迁移）").WithDetails(details...).ConfigRefs(refs...)
	case current > 0:
		return Result{}.Warn("迁移落后于代码").WithDetails(details...).ConfigRefs(refs...)
	default:
		return Result{}.Warn("无任何迁移记录").WithDetails(details...).ConfigRefs(refs...)
	}
}
