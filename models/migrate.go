package models

import (
	"context"
	"fmt"
	"time"

	"github.com/VaalaCat/frp-panel/utils/logger"
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
}

// LatestMigrationVersion 返回注册表中最新迁移的版本号，供 frpp check 等外部工具对账。
func LatestMigrationVersion() int64 {
	if len(migrationSteps) == 0 {
		return 0
	}
	return migrationSteps[len(migrationSteps)-1].version
}

// migrate 在 AutoMigrate 之后执行所有未应用的迁移。
// 执行顺序为 AutoMigrate 在前、迁移在后：AutoMigrate 先补齐模型新增的列/表/索引，
// 迁移步骤再处理删除、改类型、数据回填等破坏性变更，因此回填类迁移可以依赖目标列已存在。
func migrate(db *gorm.DB) error {
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
