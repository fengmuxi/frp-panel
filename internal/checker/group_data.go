package checker

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"gorm.io/gorm"
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
		&dataDuplicateProxyStats{baseChecker{name: "duplicate_proxy_stats", group: "data", severity: LevelMajor,
			fixHint: "同名同节点的重复统计行会让不同页面显示不同隧道的流量；用当前版本重启 master 后，下次节点上报（约 30s）会自动收敛并软删除多余行"}},
		&dataMigrationVersion{baseChecker{name: "db_migration", group: "data", severity: LevelMinor,
			fixHint: "迁移落后于代码说明 master 未用当前版本启动过或迁移中断；用当前版本 frpp 启动一次 master 即可应用未跑完的迁移"}},
		&dataSchemaDiff{baseChecker{name: "schema_diff", group: "data", severity: LevelMajor,
			fixHint: "模型与库表不一致说明版本回退或迁移中断；用当前版本 frpp 启动一次 master 会触发 AutoMigrate 修复"}},
		&dataCertExpiry{baseChecker{name: "cert_expiry", group: "data", severity: LevelMajor,
			fixHint: "证书即将过期会导致浏览器告警与节点 TLS 失败；在面板重新签发或更换证书后重启 master"}},
		&dataQuotaUsage{baseChecker{name: "quota_usage", group: "data", severity: LevelMinor,
			fixHint: "达到限额的隧道会被 proxy 自动规则停用（AutoStopped）；确认阈值是否合理或提升 quota_limit"}},
		&dataBackupFreshness{baseChecker{name: "backup_freshness", group: "data", severity: LevelMinor,
			fixHint: "迁移前会自动生成 <db>.bak-migration-* 快照；若缺失请检查迁移日志中的备份告警，并为数据目录建立周期性备份"}},
		&dataEndpointTenantScope{baseChecker{name: "endpoint_tenant_scope", group: "data", severity: LevelMajor,
			fixHint: "endpoints.user_id/tenant_id=0 的行仅 admin 可见（安全审计迁移 002）；若为正常数据请重新保存一次 WireGuard endpoint 触发归属回填"}},
		&dataSecretEncryption{baseChecker{name: "secret_encryption", group: "data", severity: LevelMajor,
			fixHint: "connect_secret 等仍为明文说明迁移 003 未完成：用当前版本 frpp 启动一次 master（或 frpp migrate up）；换 GLOBAL_SECRET 用 frpp migrate rekey"}},
	}, nil
}

// ---------------- data/endpoint_tenant_scope ----------------

type dataEndpointTenantScope struct{ baseChecker }

func (c *dataEndpointTenantScope) Run(ctx context.Context, env *Env) Result {
	refs := []string{"endpoints.user_id/tenant_id", "迁移 002 backfill_endpoint_ownership"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if !db.Migrator().HasTable(&models.Endpoint{}) {
		return Result{}.Skip("endpoints 表不存在（未使用 WireGuard 组网）").ConfigRefs(refs...)
	}

	var total, zeroOwned int64
	if err := db.Model(&models.Endpoint{}).Unscoped().Count(&total).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("统计 endpoints 失败: %v", err))
	}
	if total == 0 {
		return Result{}.Pass("endpoints 无记录").ConfigRefs(refs...)
	}
	if err := db.Model(&models.Endpoint{}).Unscoped().Where("user_id = 0 AND tenant_id = 0").Count(&zeroOwned).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("统计归属列失败: %v", err))
	}
	if zeroOwned == 0 {
		return Result{}.Pass(fmt.Sprintf("endpoints=%d 全部带租户归属", total)).ConfigRefs(refs...)
	}
	return Result{}.Warn(fmt.Sprintf("endpoints=%d 中 %d 行归属为 0（仅 admin 可见）", total, zeroOwned)).
		WithDetails("通常来自迁移 002 之前创建且无法按 client_id 回填的孤儿行；确认后删除或重新保存").ConfigRefs(refs...)
}

// ---------------- data/secret_encryption ----------------

type dataSecretEncryption struct{ baseChecker }

func (c *dataSecretEncryption) Run(ctx context.Context, env *Env) Result {
	refs := []string{"迁移 003 encrypt_secrets_at_rest", "GLOBAL_SECRET"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}

	var plainClients, plainServers int64
	_ = db.Model(&models.Client{}).Unscoped().
		Where("connect_secret != '' AND connect_secret NOT LIKE ?", "enc:v1:%").Count(&plainClients).Error
	_ = db.Model(&models.Server{}).Unscoped().
		Where("connect_secret != '' AND connect_secret NOT LIKE ?", "enc:v1:%").Count(&plainServers).Error

	var plainSMTP int64
	if db.Migrator().HasTable(&models.SMTPSettings{}) {
		_ = db.Model(&models.SMTPSettings{}).Unscoped().
			Where("password != '' AND password NOT LIKE ?", "enc:v1:%").Count(&plainSMTP).Error
	}

	total := plainClients + plainServers + plainSMTP
	if total == 0 {
		return Result{}.Pass("节点/服务端 connect_secret 与 SMTP 密码均已静态加密（enc:v1:）").ConfigRefs(refs...)
	}
	return Result{}.Warn(fmt.Sprintf("%d 个敏感字段仍为明文存储（clients=%d servers=%d smtp=%d）", total, plainClients, plainServers, plainSMTP)).
		WithDetails("库文件/备份泄露即等于节点与 SMTP 凭证泄露；用当前版本启动 master 自动执行迁移 003 加密").ConfigRefs(refs...)
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

// ---------------- data/duplicate_proxy_stats ----------------

type dataDuplicateProxyStats struct{ baseChecker }

// Run 检查 proxy_stats 中 (server_id, client_id, name) 重复的统计行：
// frps 层面 (server, frps 全名) 唯一，同一 client 下同名隧道的重复行说明发生过
// 归属漂移（历史版本按 user_id 加载旧行导致 Save 插入新行），尚未被上报收敛清理。
// 不同 client 的同名隧道是多用户共享节点的合法数据，不算重复。
func (c *dataDuplicateProxyStats) Run(ctx context.Context, env *Env) Result {
	refs := []string{"proxy_stats", "dao.AdminUpdateProxyStats 同名收敛"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if !db.Migrator().HasTable(&models.ProxyStats{}) {
		return Result{}.Skip("proxy_stats 表不存在")
	}

	var dups []struct {
		ServerID string
		ClientID string
		Name     string
		Count    int64
	}
	if err := db.Model(&models.ProxyStats{}).
		Select("server_id, client_id, name, COUNT(*) AS count").
		Group("server_id, client_id, name").
		Having("COUNT(*) > 1").
		Find(&dups).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("聚合 proxy_stats 失败: %v", err))
	}
	if len(dups) == 0 {
		return Result{}.Pass("proxy_stats 无重复统计行").ConfigRefs(refs...)
	}

	details := make([]string, 0, len(dups))
	for _, d := range dups {
		details = append(details, fmt.Sprintf("server=%s client=%s name=%s 共 %d 行", d.ServerID, d.ClientID, d.Name, d.Count))
	}
	return Result{}.Fail(fmt.Sprintf("%d 条隧道存在重复统计行", len(dups))).WithDetails(details...).ConfigRefs(refs...)
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

// ---------------- data/cert_expiry ----------------

type dataCertExpiry struct{ baseChecker }

func (c *dataCertExpiry) Run(ctx context.Context, env *Env) Result {
	refs := []string{"certs 表（CertFile/CaFile）", "conf.GetCertTemplate"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	if !db.Migrator().HasTable(&models.Cert{}) {
		return Result{}.Skip("certs 表不存在（库尚未初始化）").ConfigRefs(refs...)
	}
	certs := []models.Cert{}
	if err := db.Model(&models.Cert{}).Find(&certs).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 certs 失败: %v", err)).ConfigRefs(refs...)
	}
	if len(certs) == 0 {
		return Result{}.Pass("certs 表为空（未使用自定义证书，master 走自签链路）").ConfigRefs(refs...)
	}

	now := time.Now()
	var (
		earliest           time.Time
		soon, expiringSoon []string
		bad                []string
	)
	for i := range certs {
		cert := &certs[i]
		for kind, blob := range map[string][]byte{"cert": cert.CertFile, "ca": cert.CaFile} {
			block, _ := pem.Decode(blob)
			if block == nil || block.Type != "CERTIFICATE" {
				continue
			}
			parsed, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				bad = append(bad, fmt.Sprintf("%s(%s): 证书解析失败 %v", cert.Name, kind, err))
				continue
			}
			label := fmt.Sprintf("%s(%s) 过期于 %s", cert.Name, kind, parsed.NotAfter.Format("2006-01-02"))
			if earliest.IsZero() || parsed.NotAfter.Before(earliest) {
				earliest = parsed.NotAfter
			}
			remaining := parsed.NotAfter.Sub(now)
			switch {
			case remaining <= 0:
				expiringSoon = append(expiringSoon, label+"（已过期）")
			case remaining <= 7*24*time.Hour:
				expiringSoon = append(expiringSoon, fmt.Sprintf("%s（仅剩 %d 天）", label, int(remaining.Hours()/24)+1))
			case remaining <= 30*24*time.Hour:
				soon = append(soon, fmt.Sprintf("%s（剩 %d 天）", label, int(remaining.Hours()/24)+1))
			}
		}
	}

	details := []string{}
	if !earliest.IsZero() {
		details = append(details, fmt.Sprintf("最早过期: %s（剩 %d 天）",
			earliest.Format("2006-01-02"), int(earliest.Sub(now).Hours()/24)+1))
	}
	details = append(details, soon...)
	details = append(details, bad...)
	if len(expiringSoon) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 张证书已过期或 7 天内过期", len(expiringSoon))).
			WithDetails(append(details, expiringSoon...)...).ConfigRefs(refs...)
	}
	if len(soon) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d 张证书 30 天内过期", len(soon))).WithDetails(details...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 张证书有效期充足", len(certs))).WithDetails(details...).ConfigRefs(refs...)
}

// ---------------- data/quota_usage ----------------

type dataQuotaUsage struct{ baseChecker }

func (c *dataQuotaUsage) Run(ctx context.Context, env *Env) Result {
	refs := []string{"proxy_config.quota_type/quota_limit/quota_direction", "proxy_stats", "history_proxy_stats", "proxy.CalcProxyQuotaUsed"}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	cfgs := []models.ProxyConfig{}
	if err := db.Model(&models.ProxyConfig{}).Where("quota_type != '' AND quota_limit > 0").Find(&cfgs).Error; err != nil {
		return Result{}.Skip(fmt.Sprintf("读取 proxy_config 失败: %v", err)).ConfigRefs(refs...)
	}
	if len(cfgs) == 0 {
		return Result{}.Pass("未配置流量限额（quota_limit）").ConfigRefs(refs...)
	}

	var nearLimit, atLimit []string
	for i := range cfgs {
		cfg := &cfgs[i]
		used := quotaUsedInChecker(db, cfg.ProxyConfigEntity)
		if used <= 0 {
			continue
		}
		ratio := used * 100 / cfg.QuotaLimit
		label := fmt.Sprintf("%s@%s 已用 %.0f%%（%s / %s）",
			cfg.Name, cfg.ServerID, float64(ratio), humanBytes(used), humanBytes(cfg.QuotaLimit))
		switch {
		case ratio >= 100:
			atLimit = append(atLimit, label+"（自动规则将停用）")
		case ratio >= 80:
			nearLimit = append(nearLimit, label)
		}
	}
	if len(atLimit) > 0 {
		return Result{}.Fail(fmt.Sprintf("%d 条隧道已达流量限额", len(atLimit))).
			WithDetails(append(atLimit, nearLimit...)...).ConfigRefs(refs...)
	}
	if len(nearLimit) > 0 {
		return Result{}.Warn(fmt.Sprintf("%d 条隧道用量超过限额 80%%", len(nearLimit))).WithDetails(nearLimit...).ConfigRefs(refs...)
	}
	return Result{}.Pass(fmt.Sprintf("%d 条限额隧道用量健康", len(cfgs))).ConfigRefs(refs...)
}

// quotaUsedInChecker 只读复刻 biz/master/proxy.CalcProxyQuotaUsed 的统计口径：
// 日窗口取 frps 上报的当日计数；周/月窗口用每日快照差分（无快照回退为窗口起点以来的当日流量）。
func quotaUsedInChecker(db *gorm.DB, cfg *models.ProxyConfigEntity) int64 {
	if cfg == nil || cfg.QuotaType == "" || cfg.QuotaLimit <= 0 {
		return 0
	}
	var list []*models.ProxyStats
	if err := db.Where(&models.ProxyStats{ProxyStatsEntity: &models.ProxyStatsEntity{
		UserID:   cfg.UserID,
		TenantID: cfg.TenantID,
		ServerID: cfg.ServerID,
		Name:     cfg.Name,
	}}).Find(&list).Error; err != nil || len(list) == 0 || list[0].ProxyStatsEntity == nil {
		return 0
	}
	stats := list[0].ProxyStatsEntity

	directionSum := func(in, out int64) int64 {
		switch cfg.QuotaDirection {
		case defs.QuotaDirectionInbound:
			return in
		case defs.QuotaDirectionOutbound:
			return out
		default:
			return in + out
		}
	}

	switch cfg.QuotaType {
	case defs.QuotaTypeDay:
		return directionSum(stats.TodayTrafficIn, stats.TodayTrafficOut)
	case defs.QuotaTypeWeek, defs.QuotaTypeMonth:
		totalIn := stats.HistoryTrafficIn + stats.TodayTrafficIn
		totalOut := stats.HistoryTrafficOut + stats.TodayTrafficOut
		windowStart := quotaWindowStart(time.Now(), cfg.QuotaType)
		// 基线：窗口起点之后最早一次每日快照；无快照回退为当前累计 History（与主实现一致）
		baselineIn, baselineOut := stats.HistoryTrafficIn, stats.HistoryTrafficOut
		var snap models.HistoryProxyStats
		if err := db.Where("proxy_id = ? AND created_at >= ?", stats.ProxyID, windowStart).
			Order("created_at ASC").First(&snap).Error; err == nil {
			baselineIn, baselineOut = snap.TrafficIn, snap.TrafficOut
		}
		usedIn := totalIn - baselineIn
		if usedIn < 0 {
			usedIn = 0
		}
		usedOut := totalOut - baselineOut
		if usedOut < 0 {
			usedOut = 0
		}
		return directionSum(usedIn, usedOut)
	}
	return 0
}

// quotaWindowStart 与 biz/master/proxy.QuotaWindowStart 保持一致：自然日 / 本周一 / 本月 1 日零点。
func quotaWindowStart(now time.Time, quotaType string) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch quotaType {
	case defs.QuotaTypeWeek:
		offset := (int(now.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -offset)
	case defs.QuotaTypeMonth:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	default:
		return day
	}
}

// humanBytes 以可读单位输出字节数。
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// ---------------- data/backup_freshness ----------------

type dataBackupFreshness struct{ baseChecker }

func (c *dataBackupFreshness) Run(ctx context.Context, env *Env) Result {
	refs := []string{"frpp migrate up 自动备份", "<db>.bak-migration-*"}
	if env.Cfg.DB.Type != defs.DBTypeSQLite3 && env.Cfg.DB.Type != defs.DBTypeSqlite {
		return Result{}.Skip("仅 sqlite 具备迁移自动备份；mysql/postgres 请在 DB 宿主机检查备份策略").ConfigRefs(refs...)
	}
	dbPath := sqliteFilePath(env.Cfg.DB.DSN)
	if dbPath == "" {
		return Result{}.Skip("内存库无备份需求").ConfigRefs(refs...)
	}
	db, err := env.DB(ctx)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("数据库不可用: %v", err))
	}
	info, err := models.MigrationStatus(db)
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("读取迁移状态失败: %v", err)).ConfigRefs(refs...)
	}

	matches, err := filepath.Glob(dbPath + ".bak-migration-*")
	if err != nil {
		return Result{}.Skip(fmt.Sprintf("扫描备份文件失败: %v", err)).ConfigRefs(refs...)
	}

	if len(matches) == 0 {
		if info.CurrentVersion > 0 {
			return Result{}.Warn("库已应用迁移但没有留下备份文件（迁移先于备份机制执行，或备份生成失败）").
				WithDetails(fmt.Sprintf("库内迁移版本: %d", info.CurrentVersion),
					"建议手动备份数据目录后继续使用").ConfigRefs(refs...)
		}
		return Result{}.Pass("库未经历过迁移（全新或旧版本库），尚无迁移备份属正常").
			WithDetails("建议为数据目录建立周期性备份（例如每日复制库文件）").ConfigRefs(refs...)
	}

	newest := time.Time{}
	for _, p := range matches {
		if st, statErr := os.Stat(p); statErr == nil && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	details := []string{fmt.Sprintf("备份文件: %d 个，最近一次 %s（%d 天前）",
		len(matches), newest.Format("2006-01-02 15:04"), int(time.Since(newest).Hours()/24))}
	if info.CurrentVersion < info.LatestVersion {
		details = append(details, "存在待执行迁移，下次升级时会自动生成新备份")
	}
	return Result{}.Pass(fmt.Sprintf("迁移备份可用（最近一次 %d 天前）", int(time.Since(newest).Hours()/24))).
		WithDetails(details...).ConfigRefs(refs...)
}
