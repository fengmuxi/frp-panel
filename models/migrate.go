package models

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// MigrationRecord 是 schema 迁移版本表（schema_migrations），
// 记录已应用过的版本化迁移步骤，与 AutoMigrate 互补：
// AutoMigrate 负责加列/建表，迁移步骤负责破坏性变更与数据回填。
type MigrationRecord struct {
	Version   int64  `gorm:"primaryKey;autoIncrement:false"`
	Name      string `gorm:"type:varchar(255)"`
	AppliedAt time.Time
}

func (*MigrationRecord) TableName() string {
	return "schema_migrations"
}

// migrationStep 是一个版本化迁移步骤，编写约定（见 docs/contribute.md「数据库 schema 迁移」）：
//   - up 必须可重复执行：失败重跑或部分提交后重跑，不产生二次破坏；
//   - 优先使用 GORM Migrator API（HasIndex/DropIndex/RenameColumn 等），
//     保证 sqlite/mysql/postgres 三种方言可移植；
//   - 确需方言分支时按 db.Dialector.Name() 区分，不要写无守卫的方言 SQL。
type migrationStep struct {
	version int64
	name    string
	up      func(db *gorm.DB) error
}

// migrationSteps 按版本号严格递增注册所有迁移。
// 追加新迁移时在切片尾部续写，version 取上一条 +1。
var migrationSteps = []migrationStep{
	{version: 1, name: "drop_legacy_endpoint_index", up: dropLegacyEndpointIndex},
	{version: 2, name: "backfill_endpoint_ownership", up: backfillEndpointOwnership},
	{version: 3, name: "encrypt_secrets_at_rest", up: encryptSecretsAtRest},
}

// LatestMigrationVersion 返回注册表中最新迁移的版本号，供 frpp check 等外部工具对账。
func LatestMigrationVersion() int64 {
	if len(migrationSteps) == 0 {
		return 0
	}
	return migrationSteps[len(migrationSteps)-1].version
}

// MigrationInfo 描述单个迁移步骤及其应用状态，供 frpp migrate 等外部工具展示。
type MigrationInfo struct {
	Version int64
	Name    string
	Applied bool
}

// MigrationStatusInfo 汇总版本表的当前状态。
type MigrationStatusInfo struct {
	CurrentVersion int64             // 库内已应用的最高版本（无记录为 0）
	LatestVersion  int64             // 代码注册的最新版本
	Steps          []MigrationInfo   // 全部注册步骤及各自应用状态，按版本升序
	UnknownRecords []MigrationRecord // 版本表中存在、但注册表里没有的记录（脏数据或更高版本写入）
}

// MigrationStatus 只读地汇总迁移状态，不执行任何迁移。
// 版本表尚不存在（库由不支持迁移的旧版本创建或全新库）时，
// 返回 CurrentVersion=0、全部步骤标记为未应用，而不是报错。
func MigrationStatus(db *gorm.DB) (*MigrationStatusInfo, error) {
	info := &MigrationStatusInfo{LatestVersion: LatestMigrationVersion()}
	for _, step := range migrationSteps {
		info.Steps = append(info.Steps, MigrationInfo{Version: step.version, Name: step.name})
	}
	if !db.Migrator().HasTable(&MigrationRecord{}) {
		return info, nil
	}

	var records []MigrationRecord
	if err := db.Model(&MigrationRecord{}).Order("version").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("cannot read schema_migrations: %w", err)
	}
	registered := map[int64]bool{}
	for _, step := range migrationSteps {
		registered[step.version] = true
	}
	for _, r := range records {
		if r.Version > info.CurrentVersion {
			info.CurrentVersion = r.Version
		}
		if registered[r.Version] {
			for i := range info.Steps {
				if info.Steps[i].Version == r.Version {
					info.Steps[i].Applied = true
				}
			}
			continue
		}
		info.UnknownRecords = append(info.UnknownRecords, r)
	}
	return info, nil
}

// Migrate 在 AutoMigrate 之后执行所有未应用的迁移。
// 执行顺序为 AutoMigrate 在前、迁移在后：AutoMigrate 先补齐模型新增的列/表/索引，
// 迁移步骤再处理删除、改类型、数据回填等破坏性变更，因此回填类迁移可以依赖目标列已存在。
func Migrate(db *gorm.DB) error {
	for i := 1; i < len(migrationSteps); i++ {
		if migrationSteps[i].version <= migrationSteps[i-1].version {
			return fmt.Errorf("migration steps must be registered in strictly increasing version order: [%d_%s] after [%d_%s]",
				migrationSteps[i].version, migrationSteps[i].name,
				migrationSteps[i-1].version, migrationSteps[i-1].name)
		}
	}

	if err := db.AutoMigrate(&MigrationRecord{}); err != nil {
		return fmt.Errorf("cannot init migration table: %w", err)
	}

	var applied []MigrationRecord
	if err := db.Model(&MigrationRecord{}).Order("version DESC").Limit(1).Find(&applied).Error; err != nil {
		return fmt.Errorf("cannot read applied migration version: %w", err)
	}
	currentVersion := int64(0)
	if len(applied) > 0 {
		currentVersion = applied[0].Version
	}

	pendingVersion := int64(0)
	for _, step := range migrationSteps {
		if step.version > currentVersion {
			pendingVersion = step.version
			break
		}
	}
	if pendingVersion > 0 {
		backupDatabaseBeforeMigrate(db, pendingVersion)
	}

	ctx := context.Background()
	for _, step := range migrationSteps {
		if step.version <= currentVersion {
			continue
		}
		logger.Logger(ctx).Infof("applying db migration [%d_%s]", step.version, step.name)
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := step.up(tx); err != nil {
				return err
			}
			return tx.Create(&MigrationRecord{
				Version:   step.version,
				Name:      step.name,
				AppliedAt: time.Now(),
			}).Error
		}); err != nil {
			return fmt.Errorf("db migration [%d_%s] failed: %w", step.version, step.name, err)
		}
	}
	return nil
}

// migrationBackupKeep 每个库保留的迁移前备份份数，超出后按文件时间滚动清理。
const migrationBackupKeep = 5

// backupDatabaseBeforeMigrate 在应用待迁移前备份数据库：
// sqlite 通过 VACUUM INTO 生成一致性快照文件（单文件、含 WAL 中已提交数据）；
// mysql/postgres 仅输出人工备份提示。备份失败只警告不阻断——迁移步骤本身可重复执行，
// 不应因备份失败而卡死 master 启动。
// BackupDatabaseBeforeMigrate 在破坏性操作（迁移/rekey）前对 sqlite 文件库做
// VACUUM INTO 一致性快照；非 sqlite 或内存库跳过，mysql/postgres 提示手动备份。
// label 用于区分备份用途（如 "migration-v3"、"rekey"）。
func BackupDatabaseBeforeMigrate(db *gorm.DB, label string) {
	ctx := context.Background()
	dialector, ok := db.Dialector.(*sqlite.Dialector)
	if !ok {
		logger.Logger(ctx).Warnf("destructive db operation [%s] pending, backup your mysql/postgres database manually before continuing", label)
		return
	}
	dsn := dialector.DSN
	if strings.Contains(dsn, ":memory:") {
		return
	}
	dbPath := strings.TrimPrefix(strings.SplitN(dsn, "?", 2)[0], "file:")
	backupPath := fmt.Sprintf("%s.bak-%s-%s", dbPath, label, time.Now().Format("20060102150405"))
	if err := db.Exec("VACUUM INTO ?", backupPath).Error; err != nil {
		logger.Logger(ctx).WithError(err).Warnf("cannot backup database to [%s] before [%s], continue anyway", backupPath, label)
		return
	}
	logger.Logger(ctx).Infof("database backed up to [%s] before %s", backupPath, label)
	pruneMigrationBackups(dbPath)
}

func backupDatabaseBeforeMigrate(db *gorm.DB, targetVersion int64) {
	BackupDatabaseBeforeMigrate(db, fmt.Sprintf("migration-v%d", targetVersion))
}

// pruneMigrationBackups 滚动清理旧的迁移前备份，仅保留最近 migrationBackupKeep 份。
func pruneMigrationBackups(dbPath string) {
	matches, err := filepath.Glob(dbPath + ".bak-migration-*")
	if err != nil || len(matches) <= migrationBackupKeep {
		return
	}
	type backupFile struct {
		path    string
		modTime time.Time
	}
	files := make([]backupFile, 0, len(matches))
	for _, p := range matches {
		if st, statErr := os.Stat(p); statErr == nil {
			files = append(files, backupFile{path: p, modTime: st.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	for _, f := range files[migrationBackupKeep:] {
		if err := os.Remove(f.path); err != nil {
			logger.Logger(context.Background()).WithError(err).Warnf("cannot prune old migration backup [%s]", f.path)
		}
	}
}

// backfillEndpointOwnership 为 endpoints 表回填归属列（迁移 002，安全审计 P1-6）：
// 按 endpoints.client_id 关联 clients 表取 user_id/tenant_id；
// 无法关联的孤儿行保持 0（语义为"未知归属"，仅 admin 可见）。
// 可重复执行：只处理尚未回填（user_id=0 AND tenant_id=0）的行。
// endpoints 表不存在（未使用 WireGuard 组网的库）直接跳过。
func backfillEndpointOwnership(db *gorm.DB) error {
	if !db.Migrator().HasTable(&Endpoint{}) {
		return nil
	}
	var eps []Endpoint
	if err := db.Model(&Endpoint{}).
		Where("user_id = 0 AND tenant_id = 0").Find(&eps).Error; err != nil {
		return err
	}

	clientCache := map[string][2]int{}
	for i := range eps {
		ep := &eps[i]
		if ep.EndpointEntity == nil || ep.ClientID == "" {
			continue
		}
		owner, ok := clientCache[ep.ClientID]
		if !ok {
			var c Client
			if err := db.Model(&Client{}).Unscoped().Where("client_id = ?", ep.ClientID).
				First(&c).Error; err == nil && c.ClientEntity != nil {
				owner = [2]int{c.UserID, c.TenantID}
			}
			clientCache[ep.ClientID] = owner
		}
		if owner[0] == 0 && owner[1] == 0 {
			continue
		}
		if err := db.Model(&Endpoint{}).Where("id = ?", ep.ID).
			Updates(map[string]interface{}{"user_id": owner[0], "tenant_id": owner[1]}).Error; err != nil {
			return err
		}
	}
	return nil
}

// encryptSecretsAtRest 把存量明文敏感字段升级为密文（迁移 003，安全审计 P2）：
// clients/servers 的 connect_secret 与 smtp_settings.password。
// 可重复执行：只处理尚未带密文前缀的行；读取经 AfterFind 钩子解密，
// 后续写入经 BeforeSave 钩子加密，本迁移只负责一次性升级存量数据。
// 注意：迁移必须在 InitSecretEncryption 之后运行（master 启动链与 frpp migrate 均已保证）。
// 表不存在（新库尚未 AutoMigrate）时跳过对应部分。
func encryptSecretsAtRest(db *gorm.DB) error {
	if !secretKeySet {
		return fmt.Errorf("secret encryption key not initialized, cannot migrate secrets")
	}

	if !db.Migrator().HasTable(&Client{}) {
		return nil
	}

	var clients []Client
	if err := db.Unscoped().Find(&clients).Error; err != nil {
		return err
	}
	for i := range clients {
		c := &clients[i]
		if c.ClientEntity == nil || c.ConnectSecret == "" || strings.HasPrefix(c.ConnectSecret, secretPrefix) {
			continue
		}
		enc, err := EncryptSecret(c.ConnectSecret)
		if err != nil {
			return err
		}
		if err := db.Exec("UPDATE clients SET connect_secret = ? WHERE client_id = ?", enc, c.ClientID).Error; err != nil {
			return err
		}
	}

	var servers []Server
	if err := db.Unscoped().Find(&servers).Error; err != nil {
		return err
	}
	for i := range servers {
		s := &servers[i]
		if s.ServerEntity == nil || s.ConnectSecret == "" || strings.HasPrefix(s.ConnectSecret, secretPrefix) {
			continue
		}
		enc, err := EncryptSecret(s.ConnectSecret)
		if err != nil {
			return err
		}
		if err := db.Exec("UPDATE servers SET connect_secret = ? WHERE server_id = ?", enc, s.ServerID).Error; err != nil {
			return err
		}
	}

	if db.Migrator().HasTable(&SMTPSettings{}) {
		var smtp SMTPSettings
		if err := db.Unscoped().First(&smtp).Error; err == nil && smtp.Password != "" &&
			!strings.HasPrefix(smtp.Password, secretPrefix) {
			enc, err := EncryptSecret(smtp.Password)
			if err != nil {
				return err
			}
			if err := db.Exec("UPDATE smtp_settings SET password = ?", enc).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// dropLegacyEndpointIndex 清理 endpoints 表上被 idx_client_id_host_port_type 取代的旧唯一索引。
// 旧索引按 (client_id, host, port) 唯一，与按 type 区分的多实例 endpoint 冲突，且
// AutoMigrate 不会主动删除已存在的索引；索引不存在（新库/已清理）时 DropIndex 报错，
// 视为已处理，与收编前 Init() 中每次启动无条件尝试删除的行为保持一致。
func dropLegacyEndpointIndex(db *gorm.DB) error {
	const legacyIndexName = "idx_client_id_host_port"
	if err := db.Migrator().DropIndex(&Endpoint{}, legacyIndexName); err != nil {
		logger.Logger(context.Background()).Infof("drop legacy index [%s] skipped, maybe already dropped: %v",
			legacyIndexName, err)
	}
	return nil
}
