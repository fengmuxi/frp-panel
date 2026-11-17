package shared

import (
	"fmt"
	"os"
	"strings"

	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/internal/checker"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/glebarez/sqlite"
	"github.com/spf13/cobra"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// NewMigrateCmd 构造 `frpp migrate` 子命令组：
// 在不启动 master 的情况下查看/应用/校验 schema 迁移，
// 是"迁移失败导致 master 起不来"时的排障入口与"先迁移后重启"的运维入口。
// 与 master 启动链共用同一 runner（models.Migrate），两条路径不会漂移。
func NewMigrateCmd(cfg conf.Config) *cobra.Command {
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "database schema migration management (status/up/verify)",
		Long: `frp-panel 数据库 schema 迁移管理。

  migrate status   查看已应用/待执行迁移（只读，sqlite 以 mode=ro 打开，绝不创建新库）
  migrate up       应用待执行迁移（sqlite 自动先备份；执行前先停止 master）
  migrate verify   校验 schema_migrations 一致性（只读，失败时退出码非 0）
  migrate rekey    轮换敏感字段加密密钥（修改 GLOBAL_SECRET 后使用；执行前先停止 master）
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	migrateCmd.AddCommand(
		newMigrateStatusCmd(cfg),
		newMigrateUpCmd(cfg),
		newMigrateVerifyCmd(cfg),
		newMigrateRekeyCmd(cfg),
	)
	return migrateCmd
}

// openMigrationDB 按项目 conf 的 DB_TYPE/DB_DSN 打开数据库连接。
// status/verify 为只读用途（sqlite 以 mode=ro 打开，绝不创建新库）；
// up 需要写权限，打开方式与 master 一致。mysql/postgres 的只读约束
// 由命令本身只执行 SELECT 保证。
func openMigrationDB(cfg conf.Config, readOnly bool) (*gorm.DB, error) {
	// 迁移 003 需要加密密钥（安全审计 P2）
	models.InitSecretEncryption(cfg.App.GlobalSecret)

	switch cfg.DB.Type {
	case defs.DBTypeSQLite3, defs.DBTypeSqlite:
		dsn := cfg.DB.DSN
		if readOnly {
			dsn = checker.SQLiteReadOnlyDSN(dsn)
		}
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("open sqlite [%s] failed: %w", cfg.DB.DSN, err)
		}
		return db, nil
	case defs.DBTypeMysql:
		db, err := gorm.Open(mysql.Open(cfg.DB.DSN), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("connect mysql failed: %w", err)
		}
		return db, nil
	case defs.DBTypePostgres:
		db, err := gorm.Open(postgres.Open(cfg.DB.DSN), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("connect postgres failed: %w", err)
		}
		return db, nil
	default:
		return nil, fmt.Errorf("unsupported db type: %s", cfg.DB.Type)
	}
}

func closeMigrationDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}

// maskDSN 隐藏 DSN 的 query 部分后用于展示（sqlite pragma、mysql 口令等）。
func maskDSN(dsn string) string {
	if idx := strings.Index(dsn, "?"); idx >= 0 {
		return dsn[:idx] + "?***"
	}
	return dsn
}

func newMigrateStatusCmd(cfg conf.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "show applied/pending schema migrations (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigrationDB(cfg, true)
			if err != nil {
				return err
			}
			defer closeMigrationDB(db)

			info, err := models.MigrationStatus(db)
			if err != nil {
				return err
			}

			fmt.Printf("db type=%s dsn=%s\n", cfg.DB.Type, maskDSN(cfg.DB.DSN))
			fmt.Printf("current version: %d / latest version: %d\n\n", info.CurrentVersion, info.LatestVersion)
			fmt.Println("registered migrations:")
			for _, s := range info.Steps {
				mark := " "
				if s.Applied {
					mark = "x"
				}
				fmt.Printf("  [%s] %d_%s\n", mark, s.Version, s.Name)
			}
			if len(info.UnknownRecords) > 0 {
				fmt.Println("\nunregistered records (dirty data or written by a newer binary):")
				for _, r := range info.UnknownRecords {
					fmt.Printf("  ! %d_%s applied_at=%s\n", r.Version, r.Name, r.AppliedAt.Format("2006-01-02 15:04:05"))
				}
			}
			if pending := countPending(info); pending == 0 {
				fmt.Println("\nno pending migrations")
			} else {
				fmt.Printf("\n%d pending migrations, run `frpp migrate up` to apply\n", pending)
			}
			return nil
		},
	}
}

func newMigrateUpCmd(cfg conf.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "apply pending schema migrations (stop master first)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigrationDB(cfg, false)
			if err != nil {
				return err
			}
			defer closeMigrationDB(db)

			info, err := models.MigrationStatus(db)
			if err != nil {
				return err
			}
			// 库内版本高于代码说明二进制被回退，继续执行旧迁移步骤可能破坏新 schema
			if len(info.UnknownRecords) > 0 || info.CurrentVersion > info.LatestVersion {
				return fmt.Errorf("database has unregistered migration records (current version %d > code version %d), maybe binary was downgraded, refuse to migrate to avoid schema incompatibility",
					info.CurrentVersion, info.LatestVersion)
			}
			pending := countPending(info)
			if pending == 0 {
				fmt.Printf("no pending migrations (current version %d)\n", info.CurrentVersion)
				return nil
			}
			fmt.Printf("%d pending migrations to apply, make sure master is STOPPED first...\n", pending)
			if err := models.Migrate(db); err != nil {
				return fmt.Errorf("migration failed: %w\nfix the issue and re-run `frpp migrate up` (migrations are repeatable)", err)
			}

			after, err := models.MigrationStatus(db)
			if err != nil {
				return err
			}
			fmt.Printf("migrations applied, now at version %d\n", after.CurrentVersion)
			return nil
		},
	}
}

func newMigrateVerifyCmd(cfg conf.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "validate schema_migrations consistency (read-only, exits non-zero on failure)",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openMigrationDB(cfg, true)
			if err != nil {
				return err
			}
			defer closeMigrationDB(db)

			info, err := models.MigrationStatus(db)
			if err != nil {
				return err
			}

			var failures []string
			if len(info.UnknownRecords) > 0 {
				failures = append(failures, fmt.Sprintf("version table contains %d unregistered record(s), maybe binary was downgraded", len(info.UnknownRecords)))
			}
			if info.CurrentVersion > info.LatestVersion {
				failures = append(failures, fmt.Sprintf("database version %d is newer than code version %d, maybe binary was downgraded", info.CurrentVersion, info.LatestVersion))
			}

			fmt.Printf("db type=%s dsn=%s\n", cfg.DB.Type, maskDSN(cfg.DB.DSN))
			fmt.Printf("current version: %d / latest version: %d, pending migrations: %d\n",
				info.CurrentVersion, info.LatestVersion, countPending(info))
			if len(failures) == 0 {
				fmt.Println("[OK] schema_migrations consistency check passed")
				return nil
			}
			for _, f := range failures {
				fmt.Println("[FAIL] " + f)
			}
			os.Exit(1)
			return nil
		},
	}
}

func countPending(info *models.MigrationStatusInfo) int {
	pending := 0
	for _, s := range info.Steps {
		if !s.Applied {
			pending++
		}
	}
	return pending
}

// newMigrateRekeyCmd 轮换敏感字段加密密钥（阶段 4 运维配套）：
// 修改 GLOBAL_SECRET 前库内已有 "enc:v1:" 密文时，直接换钥会使旧密文不可解
// （节点掉线、SMTP 失效）。rekey 用 --old-secret（换钥前的旧值）把全部密文
// 解出并以当前 conf 的 GLOBAL_SECRET 重加密回写；存量明文一并升级为密文。
// 幂等可重复：已是新钥可解的行跳过，中断后可直接重跑。
func newMigrateRekeyCmd(cfg conf.Config) *cobra.Command {
	var oldSecret string
	rekeyCmd := &cobra.Command{
		Use:   "rekey",
		Short: "rotate secret-encryption key after changing GLOBAL_SECRET (stop master first)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 当前 conf 里的 GLOBAL_SECRET 即新钥；合法性沿用 master 启动基线
			if err := cfg.ValidateGlobalSecret(); err != nil {
				return fmt.Errorf("new APP_GLOBAL_SECRET invalid: %w", err)
			}
			if strings.TrimSpace(oldSecret) == "" {
				return fmt.Errorf("--old-secret is required (the GLOBAL_SECRET value used BEFORE rotation)")
			}
			if oldSecret == cfg.App.GlobalSecret {
				return fmt.Errorf("--old-secret equals current APP_GLOBAL_SECRET, nothing to rekey")
			}

			// 打开连接即注入新钥（进程密钥 = 新值），rekey 内部按行显式使用旧钥解密
			db, err := openMigrationDB(cfg, false)
			if err != nil {
				return err
			}
			defer closeMigrationDB(db)

			info, err := models.MigrationStatus(db)
			if err != nil {
				return fmt.Errorf("inspect schema_migrations failed: %w (run `frpp migrate up` first)", err)
			}
			if pending := countPending(info); pending > 0 {
				return fmt.Errorf("%d pending migrations exist, run `frpp migrate up` first", pending)
			}

			fmt.Println("rotating secret-encryption key, make sure master is STOPPED first...")
			models.BackupDatabaseBeforeMigrate(db, "rekey")
			res, err := models.RekeySecretEncryption(db, oldSecret, cfg.App.GlobalSecret)
			if err != nil {
				return fmt.Errorf("rekey failed (db unchanged rows are still decryptable with the old key; fix the issue and re-run): %w", err)
			}
			fmt.Printf("rekey done: clients=%d servers=%d smtp=%v plaintext-upgraded=%d\n",
				res.ClientsRekeyed, res.ServersRekeyed, res.SMTPRekeyed, res.SkippedPlaintext)
			fmt.Println("now update GLOBAL_SECRET in your deployment env to the new value (already reflected in this config) and start master")
			return nil
		},
	}
	rekeyCmd.Flags().StringVar(&oldSecret, "old-secret", "", "the APP_GLOBAL_SECRET value used BEFORE rotation (required)")
	_ = rekeyCmd.MarkFlagRequired("old-secret")
	return rekeyCmd
}
