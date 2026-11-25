package dao_test

import (
	"testing"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/stretchr/testify/assert"
)

// 本文件是全链路审计 2026-11-19（docs/audit-remediation-plan-2026-11-19.md P1-A）的
// 回归测试：RebuildProxyConfigFromClient 从"全删全插"改为保留式 upsert——
// 行 ID 与规则字段不再漂移，通知偏好等按行 ID 关联的数据不再变孤儿。

const rebuildTestConfigContent = `{"proxies":[
	{"name":"emby","type":"tcp","localIP":"127.0.0.1","localPort":8096,"remotePort":18096},
	{"name":"web","type":"tcp","localIP":"127.0.0.1","localPort":80,"remotePort":18080}
]}`

func newRebuildTestCtx(t *testing.T) *app.Context {
	ctx := newOwnershipTestCtx(t)
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	assert.NoError(t, db.AutoMigrate(&models.ProxyConfig{}))
	return ctx
}

func seedRebuildRow(t *testing.T, ctx *app.Context, name string, stopped bool, quotaType string) models.ProxyConfig {
	row := models.ProxyConfig{
		ProxyConfigEntity: &models.ProxyConfigEntity{
			ClientID:        "admin.c.fnnas@1",
			OriginClientID:  "admin.c.fnnas",
			ServerID:        "s1",
			Name:            name,
			Type:            "tcp",
			UserID:          1,
			TenantID:        1,
			Stopped:         stopped,
			ScheduleEnabled: true,
			ScheduleStart:   "08:00",
			ScheduleEnd:     "20:00",
			QuotaType:       quotaType,
			QuotaLimit:      1024,
		},
	}
	assert.NoError(t, ctx.GetApp().GetDBManager().GetDefaultDB().Create(&row).Error)
	return row
}

func childClientForRebuild() *models.Client {
	return &models.Client{ClientEntity: &models.ClientEntity{
		ClientID:       "admin.c.fnnas@1",
		OriginClientID: "admin.c.fnnas",
		ServerID:       "s1",
		UserID:         1,
		TenantID:       1,
		ConfigContent:  []byte(rebuildTestConfigContent),
	}}
}

// 旧行命中时：行 ID 与规则/停用字段保留，只更新连接段 Content
func TestRebuildProxyConfigPreservesRowIDAndRules(t *testing.T) {
	ctx := newRebuildTestCtx(t)
	seeded := seedRebuildRow(t, ctx, "emby", false, "day")

	err := dao.NewMutation(ctx).RebuildProxyConfigFromClient(userInfo(1, 1, "admin"), childClientForRebuild())
	assert.NoError(t, err)

	rows := []*models.ProxyConfig{}
	assert.NoError(t, ctx.GetApp().GetDBManager().GetDefaultDB().
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{ClientID: "admin.c.fnnas@1"}}).
		Find(&rows).Error)
	assert.Len(t, rows, 2) // emby 保留 + web 新增

	var kept *models.ProxyConfig
	for _, r := range rows {
		if r.Name == "emby" {
			kept = r
		}
	}
	assert.NotNil(t, kept)
	assert.Equal(t, seeded.ID, kept.ID, "行 ID 不得漂移（按行 ID 关联的通知偏好会变孤儿）")
	assert.Equal(t, "day", kept.QuotaType, "规则字段必须从旧行继承")
	assert.Equal(t, int64(1024), kept.QuotaLimit)
	assert.True(t, kept.ScheduleEnabled)
	assert.Equal(t, "08:00", kept.ScheduleStart)
	// 连接段已按新配置更新（Content 是 proxy 自身的 json，含 remotePort）
	assert.Contains(t, string(kept.Content), "18096")
}

// 本次配置中不存在的名字：未停止的行删除，已手动停止的行保留（停止语义）
func TestRebuildProxyConfigKeepsStoppedRows(t *testing.T) {
	ctx := newRebuildTestCtx(t)
	seedRebuildRow(t, ctx, "stopped-tunnel", true, "month")

	// 配置里只有 emby/web，没有 stopped-tunnel
	err := dao.NewMutation(ctx).RebuildProxyConfigFromClient(userInfo(1, 1, "admin"), childClientForRebuild())
	assert.NoError(t, err)

	rows := []*models.ProxyConfig{}
	assert.NoError(t, ctx.GetApp().GetDBManager().GetDefaultDB().
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{ClientID: "admin.c.fnnas@1"}}).
		Find(&rows).Error)

	names := map[string]bool{}
	for _, r := range rows {
		names[r.Name] = true
	}
	assert.True(t, names["stopped-tunnel"], "手动停止的行必须保留（不在配置里是停止语义的正常表现）")
	assert.True(t, names["emby"])
	assert.True(t, names["web"])
}

// 重复执行幂等：不产生重复行，行 ID 稳定
func TestRebuildProxyConfigIdempotent(t *testing.T) {
	ctx := newRebuildTestCtx(t)
	seedRebuildRow(t, ctx, "emby", false, "")

	child := childClientForRebuild()
	assert.NoError(t, dao.NewMutation(ctx).RebuildProxyConfigFromClient(userInfo(1, 1, "admin"), child))
	assert.NoError(t, dao.NewMutation(ctx).RebuildProxyConfigFromClient(userInfo(1, 1, "admin"), child))

	rows := []*models.ProxyConfig{}
	assert.NoError(t, ctx.GetApp().GetDBManager().GetDefaultDB().
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{ClientID: "admin.c.fnnas@1"}}).
		Find(&rows).Error)
	assert.Len(t, rows, 2) // emby + web，不因重复执行翻倍
}

// 非法配置内容：报错且不破坏既有行（事务回滚）
func TestRebuildProxyConfigInvalidContentRollback(t *testing.T) {
	ctx := newRebuildTestCtx(t)
	seedRebuildRow(t, ctx, "emby", false, "")
	seededID := ctx.GetApp().GetDBManager().GetDefaultDB()

	badChild := &models.Client{ClientEntity: &models.ClientEntity{
		ClientID:       "admin.c.fnnas@1",
		OriginClientID: "admin.c.fnnas",
		ServerID:       "s1",
		UserID:         1,
		TenantID:       1,
		ConfigContent:  []byte(`{"proxies":"not-an-array"}`),
	}}
	assert.Error(t, dao.NewMutation(ctx).RebuildProxyConfigFromClient(userInfo(1, 1, "admin"), badChild))

	rows := []*models.ProxyConfig{}
	assert.NoError(t, seededID.Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{ClientID: "admin.c.fnnas@1"}}).
		Find(&rows).Error)
	assert.Len(t, rows, 1, "失败时既有行不得被破坏")
}
