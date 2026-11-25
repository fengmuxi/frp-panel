package mailer

import (
	"context"
	"testing"
	"time"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// resolvePrefs 优先级链表驱动测试：
// off（全禁）> override（独立直取）> inherit/无记录（用户全局）
func newResolvePrefsTestCtx(t *testing.T) *app.Context {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(
		&models.SMTPSettings{},
		&models.UserNotifySettings{},
		&models.ResourceNotifyPrefs{},
	))

	appInstance := app.NewApp()
	dbm := models.NewDBManager(defs.DBTypeSQLite3)
	dbm.SetDB(defs.DBTypeSQLite3, defs.DBRoleDefault, db)
	appInstance.SetDBManager(dbm)
	return app.NewContext(context.Background(), appInstance)
}

func TestResolvePrefsPriorityChain(t *testing.T) {
	ctx := newResolvePrefsTestCtx(t)
	db := ctx.GetApp().GetDBManager().GetDefaultDB()

	// 用户全局：掉线开、防抖 30s、登录开
	assert.NoError(t, db.Create(&models.UserNotifySettings{
		UserID:              1,
		EnableServerOffline: true,
		EnableLoginNotify:   true,
		OfflineDelayServer:  30,
	}).Error)
	// 资源独立配置：override、上线开（掉线关）、防抖 120s
	assert.NoError(t, db.Create(&models.ResourceNotifyPrefs{
		ResourceType: models.NotifyResTypeServer,
		ResourceID:   "s1",
		UserID:       1,
		Mode:         models.NotifyModeOverride,
		EnableOnline: true,
		OfflineDelay: 120,
	}).Error)
	// 另一资源：off（全禁）
	assert.NoError(t, db.Create(&models.ResourceNotifyPrefs{
		ResourceType: models.NotifyResTypeClient,
		ResourceID:   "c1",
		UserID:       1,
		Mode:         models.NotifyModeOff,
	}).Error)

	t.Run("inherit 回退用户全局", func(t *testing.T) {
		p := resolvePrefs(ctx, models.NotifyResTypeServer, "s-untouched", 1)
		assert.True(t, p.EnableServerOffline, "无覆盖记录时应读到用户全局开关")
		assert.True(t, p.EnableLoginNotify)
		assert.Equal(t, 30, p.OfflineDelayServer)
	})

	t.Run("override 独立直取且不透传全局其他项", func(t *testing.T) {
		p := resolvePrefs(ctx, models.NotifyResTypeServer, "s1", 1)
		assert.True(t, p.EnableServerOnline, "独立配置的上线开关生效")
		assert.False(t, p.EnableServerOffline, "独立配置的掉线开关（未开）优先于全局")
		assert.Equal(t, 120, p.OfflineDelayServer, "独立防抖时长生效")
		assert.False(t, p.EnableLoginNotify, "资源级不携带登录开关")
	})

	t.Run("off 最高优先级全禁", func(t *testing.T) {
		p := resolvePrefs(ctx, models.NotifyResTypeClient, "c1", 1)
		assert.False(t, p.EnableClientOffline, "即使全局开启也一律不发")
		assert.False(t, p.EnableClientOnline)
		assert.False(t, p.EnableQuotaExceeded)
		assert.False(t, p.EnableScheduleToggle)
	})

	t.Run("资源标识为空回退用户全局", func(t *testing.T) {
		p := resolvePrefs(ctx, "", "", 1)
		assert.True(t, p.EnableServerOffline)
	})
}

// override 的防抖时长为 0 时必须回退默认 60s（不是"不防抖"）
func TestResolvePrefsOverrideDelayZeroFallsBack(t *testing.T) {
	ctx := newResolvePrefsTestCtx(t)
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	assert.NoError(t, db.Create(&models.ResourceNotifyPrefs{
		ResourceType: models.NotifyResTypeProxy,
		ResourceID:   "9",
		UserID:       1,
		Mode:         models.NotifyModeOverride,
		EnableOffline: true,
		OfflineDelay:  0,
	}).Error)

	p := resolvePrefs(ctx, models.NotifyResTypeProxy, "9", 1)
	assert.True(t, p.EnableProxyOffline)
	assert.Equal(t, 0, p.OfflineDelayProxy)
	assert.Equal(t, defs.OfflineCheckDelay, offlineDelay(p, notifyKindProxy),
		"delay=0 应回退内置默认 60s")
}

// proxy 类型 override 应同时覆盖超额与定时开关
func TestResolvePrefsProxyOverrideExtraSwitches(t *testing.T) {
	ctx := newResolvePrefsTestCtx(t)
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	assert.NoError(t, db.Create(&models.ResourceNotifyPrefs{
		ResourceType:         models.NotifyResTypeProxy,
		ResourceID:           "11",
		UserID:               1,
		Mode:                 models.NotifyModeOverride,
		EnableQuotaExceeded:  true,
		EnableScheduleToggle: false,
	}).Error)

	p := resolvePrefs(ctx, models.NotifyResTypeProxy, "11", 1)
	assert.True(t, p.EnableQuotaExceeded, "独立配置的超额提醒生效")
	assert.False(t, p.EnableScheduleToggle, "独立配置的定时提醒（未开）")
	assert.False(t, p.EnableProxyOffline, "未列出的提醒项不发送")
}

// proxyResID：正常取行主键字符串；ID=0 异常回退空串（触发用户全局兜底）
func TestProxyResID(t *testing.T) {
	assert.Equal(t, "42", proxyResID(&models.ProxyConfig{Model: &gorm.Model{ID: 42}}))
	assert.Equal(t, "", proxyResID(&models.ProxyConfig{Model: &gorm.Model{ID: 0}}))
	assert.Equal(t, "", proxyResID(nil))
}

// 防抖取值链：override delay>0 直接用
func TestOfflineDelayOverridePositive(t *testing.T) {
	p := &models.UserNotifySettings{OfflineDelayServer: 45}
	assert.Equal(t, 45*time.Second, offlineDelay(p, notifyKindServer))
}
