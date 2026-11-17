package models_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// hasSQLiteIndex 直接查 sqlite_master 判断索引是否存在（测试仅覆盖 SQLite 内存库）
func hasSQLiteIndex(t *testing.T, db *gorm.DB, name string) bool {
	var cnt int64
	assert.NoError(t, db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&cnt).Error)
	return cnt > 0
}

func newTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	return db
}

func runDBInit(db *gorm.DB) {
	// 迁移 003（敏感字段加密）依赖加密密钥，测试用固定值初始化
	models.InitSecretEncryption("test-global-secret")
	mgr := models.NewDBManager(defs.DBTypeSQLite3)
	mgr.SetDB(defs.DBTypeSQLite3, defs.DBRoleDefault, db)
	mgr.Init()
}

func assertMigrationsApplied(t *testing.T, db *gorm.DB, wantVersions []int64, wantNames []string) {
	var records []models.MigrationRecord
	assert.NoError(t, db.Model(&models.MigrationRecord{}).Order("version").Find(&records).Error)
	assert.Len(t, records, len(wantVersions))
	for i := range records {
		if i < len(wantVersions) {
			assert.Equal(t, wantVersions[i], records[i].Version)
			assert.Equal(t, wantNames[i], records[i].Name)
		}
	}
}

var (
	wantVersions = []int64{1, 2, 3}
	wantNames    = []string{"drop_legacy_endpoint_index", "backfill_endpoint_ownership", "encrypt_secrets_at_rest"}
)

// 新库：完整走一遍 Init（AutoMigrate 全部模型 + 版本化迁移），
// schema_migrations 记录全部迁移，且不遗留旧索引。
func TestMigrateFreshDB(t *testing.T) {
	db := newTestDB(t)
	runDBInit(db)

	assertMigrationsApplied(t, db, wantVersions, wantNames)
	assert.False(t, hasSQLiteIndex(t, db, "idx_client_id_host_port"))
	assert.True(t, db.Migrator().HasIndex(&models.Endpoint{}, "idx_client_id_host_port_type"))
}

// 旧库模拟：手工按历史遗留结构建 endpoints 表并带上旧唯一索引，
// Init 后旧索引应被迁移 1 清理，新索引建好且数据完好。
func TestMigrateLegacyEndpointIndex(t *testing.T) {
	db := newTestDB(t)

	// 仅含旧索引涉及的列，其余列交给 AutoMigrate 补齐
	assert.NoError(t, db.Exec(`
		CREATE TABLE endpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id VARCHAR(255),
			host TEXT,
			port INTEGER
		)`).Error)
	assert.NoError(t, db.Exec(`
		CREATE UNIQUE INDEX idx_client_id_host_port ON endpoints(client_id, host, port)`).Error)
	assert.NoError(t, db.Exec(`
		INSERT INTO endpoints (client_id, host, port) VALUES ('client-1', '10.0.0.1', 51820)`).Error)

	assert.True(t, hasSQLiteIndex(t, db, "idx_client_id_host_port"))
	runDBInit(db)

	assert.False(t, hasSQLiteIndex(t, db, "idx_client_id_host_port"))
	assert.True(t, db.Migrator().HasIndex(&models.Endpoint{}, "idx_client_id_host_port_type"))
	assertMigrationsApplied(t, db, wantVersions, wantNames)

	var cnt int64
	assert.NoError(t, db.Model(&models.Endpoint{}).Unscoped().Count(&cnt).Error)
	assert.Equal(t, int64(1), cnt)
}

// 幂等：重复 Init 不再重复执行迁移，版本记录保持不变。
func TestMigrateIdempotent(t *testing.T) {
	db := newTestDB(t)
	runDBInit(db)
	runDBInit(db)

	var records []models.MigrationRecord
	assert.NoError(t, db.Model(&models.MigrationRecord{}).Order("version").Find(&records).Error)
	assert.Len(t, records, len(wantVersions))
	assert.Equal(t, int64(3), models.LatestMigrationVersion())
}

// 文件库迁移：应用首个迁移前应生成 VACUUM INTO 一致性快照，
// 备份本身可作为独立只读库打开且包含完整建表结果（迁移记录为空——备份发生在应用前）。
func TestMigrateSQLiteBackup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	assert.NoError(t, err)
	// Windows 下文件库被连接持有会导致 TempDir 清理失败，测试结束前关闭
	sqlDB, err := db.DB()
	assert.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	runDBInit(db)

	matches, err := filepath.Glob(dbPath + ".bak-migration-v1-*")
	assert.NoError(t, err)
	assert.Len(t, matches, 1)

	backupDB, err := gorm.Open(sqlite.Open(matches[0]+"?mode=ro"), &gorm.Config{})
	assert.NoError(t, err)
	assert.True(t, backupDB.Migrator().HasTable(&models.Endpoint{}))
	var cnt int64
	assert.NoError(t, backupDB.Model(&models.MigrationRecord{}).Count(&cnt).Error)
	assert.Equal(t, int64(0), cnt)
	backupSQLDB, err := backupDB.DB()
	assert.NoError(t, err)
	assert.NoError(t, backupSQLDB.Close())
}

// 备份滚动清理：旧备份超过保留份数时按文件时间清理，新备份始终保留。
func TestMigrateBackupRotation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	for i := 0; i < 7; i++ {
		p := fmt.Sprintf("%s.bak-migration-v1-%02d", dbPath, i)
		assert.NoError(t, os.WriteFile(p, []byte("old"), 0o600))
		older := time.Now().Add(-time.Duration(8-i) * time.Hour)
		assert.NoError(t, os.Chtimes(p, older, older))
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	assert.NoError(t, err)
	sqlDB, err := db.DB()
	assert.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	runDBInit(db)

	matches, err := filepath.Glob(dbPath + ".bak-migration-*")
	assert.NoError(t, err)
	// 保留份数为 5（models/migrate.go 的 migrationBackupKeep），新备份 + 4 个最新旧备份
	assert.Len(t, matches, 5)
}

// MigrationStatus：正常状态下报告当前版本、步骤应用标记，且无脏记录。
func TestMigrationStatus(t *testing.T) {
	db := newTestDB(t)
	runDBInit(db)

	info, err := models.MigrationStatus(db)
	assert.NoError(t, err)
	assert.Equal(t, int64(3), info.CurrentVersion)
	assert.Equal(t, int64(3), info.LatestVersion)
	assert.Len(t, info.Steps, 3)
	for _, s := range info.Steps {
		assert.True(t, s.Applied)
	}
	assert.Empty(t, info.UnknownRecords)
}

// MigrationStatus：版本表存在但注册表没有的记录应被识别为脏数据
// （典型场景：二进制回退后库内版本高于代码）。
func TestMigrationStatusUnknownRecord(t *testing.T) {
	db := newTestDB(t)
	runDBInit(db)
	assert.NoError(t, db.Create(&models.MigrationRecord{
		Version: 99, Name: "future_step", AppliedAt: time.Now(),
	}).Error)

	info, err := models.MigrationStatus(db)
	assert.NoError(t, err)
	assert.Equal(t, int64(99), info.CurrentVersion)
	assert.Len(t, info.UnknownRecords, 1)
	if len(info.UnknownRecords) == 1 {
		assert.Equal(t, int64(99), info.UnknownRecords[0].Version)
	}
}

// MigrationStatus：版本表尚不存在时返回版本 0 且全部步骤未应用，而不是报错。
func TestMigrationStatusNoTable(t *testing.T) {
	db := newTestDB(t)

	info, err := models.MigrationStatus(db)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), info.CurrentVersion)
	assert.Equal(t, int64(3), info.LatestVersion)
	assert.Len(t, info.Steps, 3)
	assert.False(t, info.Steps[0].Applied)
	assert.Empty(t, info.UnknownRecords)
}

// 迁移 002（安全审计 P1-6）：endpoints 归属列按 client_id 关联 clients 回填；
// 无法关联的孤儿行保持 0（仅 admin 可见）。
// 说明：不模拟"缺列的裸表"，因为生产环境里旧库是旧模型的完整结构，
// 新列由 AutoMigrate 原生 ALTER ADD（安全、不重建）；这里模拟的是
// "列已存在但归属为 0、迁移记录缺失"的待回填状态。
func TestMigrateEndpointOwnershipBackfill(t *testing.T) {
	db := newTestDB(t)
	runDBInit(db)

	// 1. 模拟存量状态：归属列归零 + 已应用的迁移记录清除
	assert.NoError(t, db.Exec(`UPDATE endpoints SET user_id = 0, tenant_id = 0`).Error)
	assert.NoError(t, db.Exec(`DELETE FROM schema_migrations WHERE version >= 2`).Error)

	// 2. 种回填来源 client + 一条孤儿 endpoint 行
	assert.NoError(t, db.AutoMigrate(&models.Client{}))
	assert.NoError(t, db.Create(&models.Client{
		ClientEntity: &models.ClientEntity{ClientID: "client-1", UserID: 7, TenantID: 7},
	}).Error)
	assert.NoError(t, db.Create(&models.Endpoint{
		EndpointEntity: &models.EndpointEntity{ClientID: "client-1", Host: "h1", Port: 1},
	}).Error)
	assert.NoError(t, db.Create(&models.Endpoint{
		EndpointEntity: &models.EndpointEntity{ClientID: "ghost", Host: "h2", Port: 2},
	}).Error)

	// 3. 再次执行迁移：v2 重新应用并回填
	runDBInit(db)

	var userID, tenantID int
	assert.NoError(t, db.Raw(`SELECT user_id, tenant_id FROM endpoints WHERE client_id = 'client-1'`).Row().Scan(&userID, &tenantID))
	assert.Equal(t, 7, userID)
	assert.Equal(t, 7, tenantID)

	var orphanUser int
	assert.NoError(t, db.Raw(`SELECT user_id FROM endpoints WHERE client_id = 'ghost'`).Row().Scan(&orphanUser))
	assert.Equal(t, 0, orphanUser, "孤儿行保持 0（仅 admin 可见）")
}
