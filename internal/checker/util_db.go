package checker

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openDB 按项目 conf 的 DB_TYPE/DB_DSN 打开数据库连接。
//
// 与 cmd/frpp/shared/providers.go 中 NewDBManager 保持一致的驱动选择；
// sqlite 使用只读模式（mode=ro）打开，保证 check 进程绝不写库、
// 也不会因 DSN 指向的文件不存在而悄悄创建新库。
func openDB(cfg conf.Config) (*gorm.DB, error) {
	switch cfg.DB.Type {
	case defs.DBTypeSQLite3, defs.DBTypeSqlite:
		dsn := sqliteReadOnlyDSN(cfg.DB.DSN)
		return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	case defs.DBTypeMysql:
		return gorm.Open(mysql.Open(cfg.DB.DSN), &gorm.Config{})
	case defs.DBTypePostgres:
		return gorm.Open(postgres.Open(cfg.DB.DSN), &gorm.Config{})
	default:
		return nil, fmt.Errorf("unsupported db type: %s", cfg.DB.Type)
	}
}

// sqliteReadOnlyDSN 将 sqlite DSN 转换为只读打开。
// 项目默认 DSN 形如 `/data/data.db?_pragma=journal_mode(WAL)`，
// 我们保留已有 pragma，仅追加 mode=ro（glebarez/sqlite 基于 modernc，支持该参数）。
func sqliteReadOnlyDSN(dsn string) string {
	if strings.Contains(dsn, ":memory:") {
		return dsn
	}
	// 拆出纯路径部分（去掉 query）
	path := dsn
	query := ""
	if idx := strings.Index(dsn, "?"); idx >= 0 {
		path = dsn[:idx]
		query = dsn[idx+1:]
	}
	if !strings.HasPrefix(path, "file:") {
		path = "file:" + path
	}
	sep := "?"
	if query != "" {
		sep = "&"
	}
	// mode=ro 之后必须用 & 续接；若复用 sep，裸路径 DSN（无 query）会拼出 `?mode=ro?_pragma=...` 的畸形 URI
	return path + sep + "mode=ro" + "&" + "_pragma=busy_timeout(5000)"
}

// SQLiteFallbackHint 在主 DSN 打不开时，给出与 master 相同的回退逻辑提示
// （master 会在目录不可用时退回到可执行文件同目录）。
func SQLiteFallbackHint(cfg conf.Config) string {
	base := filepath.Base(cfg.DB.DSN)
	if q := strings.Index(base, "?"); q >= 0 {
		base = base[:q]
	}
	return fmt.Sprintf("master 无法创建目录时会把库文件回退到工作目录 %s；请确认实际数据位置后设置 DB_DSN", base)
}
