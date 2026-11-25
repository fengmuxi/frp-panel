package dao_test

import (
	"context"
	"testing"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func newResourceNotifyTestCtx(t *testing.T) *app.Context {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(
		&models.ResourceNotifyPrefs{},
		&models.ProxyConfig{},
		&models.Client{},
		&models.Server{},
	))

	appInstance := app.NewApp()
	dbm := models.NewDBManager(defs.DBTypeSQLite3)
	dbm.SetDB(defs.DBTypeSQLite3, defs.DBRoleDefault, db)
	appInstance.SetDBManager(dbm)
	return app.NewContext(context.Background(), appInstance)
}

// 无记录必须返回 Mode=inherit 的零值（默认跟随用户全局，不报错）——这是向后兼容的根基
func TestGetResourceNotifyPrefsDefaultInherit(t *testing.T) {
	ctx := newResourceNotifyTestCtx(t)
	q := dao.NewQuery(ctx)

	p, err := q.GetResourceNotifyPrefs(models.NotifyResTypeClient, "nonexistent")
	assert.NoError(t, err)
	assert.Equal(t, models.NotifyModeInherit, p.Mode, "无记录默认 inherit")

	// 空标识同样回退 inherit，不查库不报错
	p, err = q.GetResourceNotifyPrefs("", "")
	assert.NoError(t, err)
	assert.Equal(t, models.NotifyModeInherit, p.Mode)
}

// upsert 往返：同一 (type, id) 覆盖保存，不同资源互不影响
func TestSaveResourceNotifyPrefsUpsert(t *testing.T) {
	ctx := newResourceNotifyTestCtx(t)
	q := dao.NewQuery(ctx)
	m := dao.NewMutation(ctx)

	assert.NoError(t, m.SaveResourceNotifyPrefs(&models.ResourceNotifyPrefs{
		ResourceType:  models.NotifyResTypeClient,
		ResourceID:    "c1",
		UserID:        1,
		Mode:          models.NotifyModeOverride,
		EnableOffline: true,
		OfflineDelay:  120,
	}))
	// 再次保存覆盖为 off（upsert 而非重复插入）
	assert.NoError(t, m.SaveResourceNotifyPrefs(&models.ResourceNotifyPrefs{
		ResourceType: models.NotifyResTypeClient,
		ResourceID:   "c1",
		UserID:       1,
		Mode:         models.NotifyModeOff,
	}))
	// 另一资源独立
	assert.NoError(t, m.SaveResourceNotifyPrefs(&models.ResourceNotifyPrefs{
		ResourceType: models.NotifyResTypeProxy,
		ResourceID:   "42",
		UserID:       2,
		Mode:         models.NotifyModeOverride,
	}))

	p, err := q.GetResourceNotifyPrefs(models.NotifyResTypeClient, "c1")
	assert.NoError(t, err)
	assert.Equal(t, models.NotifyModeOff, p.Mode, "第二次保存应覆盖第一次")
	assert.False(t, p.EnableOffline, "off 模式保存后开关为 false")

	p, err = q.GetResourceNotifyPrefs(models.NotifyResTypeProxy, "42")
	assert.NoError(t, err)
	assert.Equal(t, models.NotifyModeOverride, p.Mode)
	assert.Equal(t, 2, p.UserID, "UserID 应为资源属主")

	var count int64
	assert.NoError(t, ctx.GetApp().GetDBManager().GetDefaultDB().
		Model(&models.ResourceNotifyPrefs{}).Count(&count).Error)
	assert.EqualValues(t, 2, count, "upsert 不应产生重复行")
}

// 级联清理：删除资源时对应通知覆盖记录必须被移除（避免孤儿记录）
func TestResourceNotifyPrefsCascadeOnDelete(t *testing.T) {
	ctx := newResourceNotifyTestCtx(t)
	q := dao.NewQuery(ctx)
	m := dao.NewMutation(ctx)

	assert.NoError(t, m.SaveResourceNotifyPrefs(&models.ResourceNotifyPrefs{
		ResourceType: models.NotifyResTypeProxy,
		ResourceID:   "7",
		UserID:       1,
		Mode:         models.NotifyModeOff,
	}))

	// 模拟资源删除后的级联（DeleteClient/DeleteServer/DeleteProxyConfig 内部走同一入口）
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	assert.NoError(t, db.Where(&models.ResourceNotifyPrefs{
		ResourceType: models.NotifyResTypeProxy,
		ResourceID:   "7",
	}).Delete(&models.ResourceNotifyPrefs{}).Error)

	p, err := q.GetResourceNotifyPrefs(models.NotifyResTypeProxy, "7")
	assert.NoError(t, err)
	assert.Equal(t, models.NotifyModeInherit, p.Mode, "删除后应回到默认 inherit")
}

// proxy 归属断言：owner/admin 通过，他人资源与非法 ID 拒绝
func TestAssertProxyOwnershipScope(t *testing.T) {
	ctx := newResourceNotifyTestCtx(t)
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	cfg := &models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
		Name: "t1", UserID: 1, TenantID: 1,
	}}
	assert.NoError(t, db.Create(cfg).Error)

	assert.NoError(t, dao.AssertProxyOwnership(ctx, userInfo(1, 1, defs.UserRole_Normal), cfg.ID))
	assert.Error(t, dao.AssertProxyOwnership(ctx, userInfo(2, 2, defs.UserRole_Normal), cfg.ID),
		"跨租户访问他人隧道必须被拒绝")
	assert.NoError(t, dao.AssertProxyOwnership(ctx, userInfo(9, 9, defs.UserRole_Admin), cfg.ID),
		"admin 放行")
	assert.Error(t, dao.AssertProxyOwnership(ctx, userInfo(1, 1, defs.UserRole_Normal), 0),
		"非法行主键必须被拒绝")
}
