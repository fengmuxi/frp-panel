package models_test

import (
	"os"
	"testing"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 迁移方言回归：在真实 MySQL/PostgreSQL 上验证 runner、版本表与迁移 001 的行为
//（sqlite 之外的两种生产方言）。本地无对应环境变量时自动 skip；
// CI 通过 .github/workflows/db-migration-check.yml 注入 TEST_MYSQL_DSN / TEST_POSTGRES_DSN。

func runDialectMigrateTest(t *testing.T, db *gorm.DB) {
	// 迁移 003（敏感字段加密）依赖加密密钥；本测试在 CI 中以 -run 单独执行，
	// 不会经过 sqlite 测试里的初始化，先注入与 sqlite 测试一致的固定值
	models.InitSecretEncryption("test-global-secret")

	// 幂等：清理上一次 CI 运行遗留的版本表
	assert.NoError(t, db.Exec("DROP TABLE IF EXISTS schema_migrations").Error)

	// 首次执行：全部迁移应用并记录
	assert.NoError(t, models.Migrate(db))
	info, err := models.MigrationStatus(db)
	assert.NoError(t, err)
	assert.Equal(t, models.LatestMigrationVersion(), info.CurrentVersion)
	assert.Empty(t, info.UnknownRecords)
	for _, s := range info.Steps {
		assert.True(t, s.Applied, "step %d_%s should be applied", s.Version, s.Name)
	}

	// 重复执行幂等：版本不再推进
	assert.NoError(t, models.Migrate(db))
	info2, err := models.MigrationStatus(db)
	assert.NoError(t, err)
	assert.Equal(t, info.CurrentVersion, info2.CurrentVersion)
	assert.Len(t, info2.Steps, len(info.Steps))
}

func TestMigrateDialectMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set, skip mysql dialect test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	assert.NoError(t, err)
	sqlDB, err := db.DB()
	assert.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	runDialectMigrateTest(t, db)
}

func TestMigrateDialectPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set, skip postgres dialect test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	assert.NoError(t, err)
	sqlDB, err := db.DB()
	assert.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	runDialectMigrateTest(t, db)
}
