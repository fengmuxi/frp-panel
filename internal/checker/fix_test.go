package checker

import (
	"context"
	"testing"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func newFixTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(
		&models.Client{}, &models.Server{}, &models.ProxyConfig{}, &models.ProxyStats{},
		&models.HistoryProxyStats{}, &models.MigrationRecord{},
	))
	return db
}

// 孤儿隧道修复：dry-run 仅列出；--destructive 等价的 destructive=false 才软删除。
func TestFixOrphanProxyConfigs(t *testing.T) {
	db := newFixTestDB(t)
	assert.NoError(t, db.Create(&models.Client{
		ClientEntity: &models.ClientEntity{ClientID: "c1", TenantID: 1, UserID: 1},
	}).Error)
	assert.NoError(t, db.Create(&models.ProxyConfig{
		ProxyConfigEntity: &models.ProxyConfigEntity{Name: "orphan", ClientID: "gone", ServerID: "s1"},
	}).Error)

	env := &Env{}
	// dry-run：列出但不删除
	outcomes := ApplyFixes(context.Background(), db, env, true)
	orphan := outcomes[0]
	assert.Equal(t, "orphan_proxy_configs", orphan.Fixer)
	assert.Equal(t, FixStatusListed, orphan.Status)
	var cnt int64
	db.Model(&models.ProxyConfig{}).Count(&cnt)
	assert.Equal(t, int64(1), cnt)

	// destructive：实际软删除
	outcomes = ApplyFixes(context.Background(), db, env, false)
	assert.Equal(t, FixStatusApplied, outcomes[0].Status)
	db.Model(&models.ProxyConfig{}).Count(&cnt)
	assert.Equal(t, int64(0), cnt)
	db.Unscoped().Model(&models.ProxyConfig{}).Count(&cnt)
	assert.Equal(t, int64(1), cnt, "软删除记录仍应在审计中可见")
}

// sort_index 归一化：同租户下重复的非零序号按现有顺序重排为 1..N；0（未设置）不动。
func TestFixSortIndexDuplicates(t *testing.T) {
	db := newFixTestDB(t)
	base := time.Now()
	newClient := func(id string, sort int, createdAt time.Time) {
		assert.NoError(t, db.Create(&models.Client{
			ClientEntity: &models.ClientEntity{
				ClientID: id, TenantID: 1, UserID: 1,
				SortIndex: sort, CreatedAt: createdAt, UpdatedAt: createdAt,
			},
		}).Error)
	}
	newClient("dup-a", 5, base)
	newClient("dup-b", 5, base.Add(time.Second))
	newClient("unset", 0, base)

	env := &Env{}
	outcomes := ApplyFixes(context.Background(), db, env, false)
	var listed []FixOutcome
	for _, o := range outcomes {
		if o.Fixer == "sort_index_duplicates" && o.Status == FixStatusApplied {
			listed = append(listed, o)
		}
	}
	assert.NotEmpty(t, listed, "应产生 sort_index 归一化动作")

	var after []models.Client
	assert.NoError(t, db.Model(&models.Client{}).Order("sort_index ASC, created_at ASC").Find(&after).Error)
	got := map[string]int{}
	for _, c := range after {
		got[c.ClientID] = c.SortIndex
	}
	assert.Equal(t, 1, got["dup-a"], "按创建时间先到的重排为 1")
	assert.Equal(t, 2, got["dup-b"])
	assert.Equal(t, 0, got["unset"], "未设置（0）不应被改动")
}

// 废弃环境变量修复器：仅生成 unset 清单，永不产生写操作。
func TestFixDeprecatedEnvVars(t *testing.T) {
	t.Setenv("MASTER_COMPATIBLE_MODE", "1")
	env := &Env{}
	outcomes := ApplyFixes(context.Background(), newFixTestDB(t), env, true)
	var de *FixOutcome
	for i := range outcomes {
		if outcomes[i].Fixer == "deprecated_env_vars" {
			de = &outcomes[i]
		}
	}
	assert.NotNil(t, de)
	assert.Equal(t, FixStatusListed, de.Status)
	assert.Contains(t, de.Detail[1], "unset MASTER_COMPATIBLE_MODE")
}
