package models_test

import (
	"testing"

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
	mgr := models.NewDBManager(defs.DBTypeSQLite3)
	mgr.SetDB(defs.DBTypeSQLite3, defs.DBRoleDefault, db)
	mgr.Init()
}

func assertMigrationRecorded(t *testing.T, db *gorm.DB, version int64, name string) {
	var records []models.MigrationRecord
	assert.NoError(t, db.Model(&models.MigrationRecord{}).Order("version").Find(&records).Error)
	assert.Len(t, records, 1)
	if len(records) == 1 {
		assert.Equal(t, version, records[0].Version)
		assert.Equal(t, name, records[0].Name)
	}
}

// 新库：完整走一遍 Init（AutoMigrate 全部模型 + 版本化迁移），
// schema_migrations 记录迁移 1，且不遗留旧索引。
func TestMigrateFreshDB(t *testing.T) {
	db := newTestDB(t)
	runDBInit(db)

	assertMigrationRecorded(t, db, 1, "drop_legacy_endpoint_index")
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
	assertMigrationRecorded(t, db, 1, "drop_legacy_endpoint_index")

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
	assert.Len(t, records, 1)
	if len(records) == 1 {
		assert.Equal(t, int64(1), records[0].Version)
	}
	assert.Equal(t, int64(1), models.LatestMigrationVersion())
}
