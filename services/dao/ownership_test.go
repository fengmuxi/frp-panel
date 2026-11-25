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

// 本文件是安全审计 P0-1 / P0-3 的攻击复现与修复验证：
// 资源归属断言与 worker 附加的租户过滤。

func newOwnershipTestCtx(t *testing.T) *app.Context {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&models.Client{}, &models.Server{}, &models.Endpoint{}))

	appInstance := app.NewApp()
	dbm := models.NewDBManager(defs.DBTypeSQLite3)
	dbm.SetDB(defs.DBTypeSQLite3, defs.DBRoleDefault, db)
	appInstance.SetDBManager(dbm)
	return app.NewContext(context.Background(), appInstance)
}

func userInfo(userID, tenantID int, role string) models.UserInfo {
	return &models.UserEntity{UserID: userID, TenantID: tenantID, Role: role}
}

func seedOwnershipData(t *testing.T, ctx *app.Context) {
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	assert.NoError(t, db.Create(&models.Client{
		ClientEntity: &models.ClientEntity{ClientID: "c1", UserID: 1, TenantID: 1},
	}).Error)
	assert.NoError(t, db.Create(&models.Client{
		ClientEntity: &models.ClientEntity{ClientID: "c2", UserID: 2, TenantID: 2},
	}).Error)
	assert.NoError(t, db.Create(&models.Server{
		ServerEntity: &models.ServerEntity{ServerID: "s1", UserID: 1, TenantID: 1},
	}).Error)
}

// 复现 P0-1：PTY/日志流/clientsstatus 等路由的归属断言——
// owner 与 admin 通过，跨租户与不存在的资源一律拒绝。
func TestAssertClientOwnership(t *testing.T) {
	ctx := newOwnershipTestCtx(t)
	seedOwnershipData(t, ctx)

	assert.NoError(t, dao.AssertClientOwnership(ctx, userInfo(1, 1, defs.UserRole_Normal), "c1"))
	assert.Error(t, dao.AssertClientOwnership(ctx, userInfo(2, 2, defs.UserRole_Normal), "c1"),
		"跨租户访问其他用户的 client 必须被拒绝")
	assert.Error(t, dao.AssertClientOwnership(ctx, userInfo(1, 1, defs.UserRole_Normal), "ghost"))
	assert.Error(t, dao.AssertClientOwnership(ctx, userInfo(1, 1, defs.UserRole_Normal), ""))
	assert.NoError(t, dao.AssertClientOwnership(ctx, userInfo(9, 9, defs.UserRole_Admin), "c2"),
		"admin 角色放行（跨租户运维）")
}

func TestAssertServerOwnership(t *testing.T) {
	ctx := newOwnershipTestCtx(t)
	seedOwnershipData(t, ctx)

	assert.NoError(t, dao.AssertServerOwnership(ctx, userInfo(1, 1, defs.UserRole_Normal), "s1"))
	assert.Error(t, dao.AssertServerOwnership(ctx, userInfo(2, 2, defs.UserRole_Normal), "s1"))
	assert.NoError(t, dao.AssertServerOwnership(ctx, userInfo(9, 9, defs.UserRole_Admin), "s1"))
}

// 复现 P0-3：worker 附加不得命中其他租户的 client（原实现 `client_id IN ?` 无租户过滤）。
func TestGetClientsByClientIDsTenantFilter(t *testing.T) {
	ctx := newOwnershipTestCtx(t)
	seedOwnershipData(t, ctx)
	q := dao.NewQuery(ctx)

	cs, err := q.GetClientsByClientIDs(userInfo(1, 1, defs.UserRole_Normal), []string{"c1", "c2"})
	assert.NoError(t, err)
	assert.Len(t, cs, 1, "普通用户只能命中自己的 client")
	if len(cs) == 1 {
		assert.Equal(t, "c1", cs[0].ClientID)
	}

	cs, err = q.GetClientsByClientIDs(userInfo(9, 9, defs.UserRole_Admin), []string{"c1", "c2"})
	assert.NoError(t, err)
	assert.Len(t, cs, 2, "admin 放行全量")

	_, err = q.GetClientsByClientIDs(nil, []string{"c1"})
	assert.Error(t, err, "无用户信息必须拒绝")
}

// 复现 P1-6：endpoints 的 CRUD/列表此前完全无租户过滤，任意租户可操作其他租户的组网端点。
func TestEndpointScoping(t *testing.T) {
	ctx := newOwnershipTestCtx(t)
	seedOwnershipData(t, ctx)
	q := dao.NewQuery(ctx)
	m := dao.NewMutation(ctx)

	user1 := userInfo(1, 1, defs.UserRole_Normal)
	user2 := userInfo(2, 2, defs.UserRole_Normal)

	// CreateEndpoint 从关联的 client 解析归属：user1 挂自己的 c1 → 归属 (1,1)
	ent := &models.EndpointEntity{ClientID: "c1", Host: "h1", Port: 1}
	assert.NoError(t, m.CreateEndpoint(user1, ent))
	assert.Equal(t, 1, ent.UserID)
	assert.Equal(t, 1, ent.TenantID)

	// user1 想把自己的 endpoint 挂到 user2 的 client c2 上 → 必须被拒（P0-3 同源问题）
	assert.Error(t, m.CreateEndpoint(user1, &models.EndpointEntity{ClientID: "c2", Host: "h3", Port: 3}))

	// user2 创建自己的 endpoint
	assert.NoError(t, m.CreateEndpoint(user2, &models.EndpointEntity{ClientID: "c2", Host: "h2", Port: 2}))

	// 列表收敛：各自只能看到自己的
	list1, err := q.ListEndpoints(user1, 1, 10)
	assert.NoError(t, err)
	assert.Len(t, list1, 1)
	list2, err := q.ListEndpoints(user2, 1, 10)
	assert.NoError(t, err)
	assert.Len(t, list2, 1)
	listAdmin, err := q.ListEndpoints(userInfo(9, 9, defs.UserRole_Admin), 1, 10)
	assert.NoError(t, err)
	assert.Len(t, listAdmin, 2)

	ep1ID := list1[0].ID

	// 跨租户 Get 必须拒绝
	_, err = q.GetEndpointByID(user2, ep1ID)
	assert.Error(t, err)
	_, err = q.GetEndpointByID(user1, ep1ID)
	assert.NoError(t, err)

	// 跨租户 Update 必须拒绝；Delete 因 scope 不命中任何行而静默无效果（不报错），
	// 数据是否完好由下方 still 断言验证
	assert.Error(t, m.UpdateEndpoint(user2, ep1ID, &models.EndpointEntity{Host: "hacked", Port: 9}))
	assert.NoError(t, m.DeleteEndpoint(user2, ep1ID))
	still, err := q.GetEndpointByID(user1, ep1ID)
	assert.NoError(t, err)
	assert.Equal(t, "h1", still.Host, "跨租户改删不得生效")

	// owner 正常更新
	assert.NoError(t, m.UpdateEndpoint(user1, ep1ID, &models.EndpointEntity{Host: "h1-new", Port: 1}))
	updated, err := q.GetEndpointByID(user1, ep1ID)
	assert.NoError(t, err)
	assert.Equal(t, "h1-new", updated.Host)

	// owner 删除生效
	assert.NoError(t, m.DeleteEndpoint(user1, ep1ID))
	_, err = q.GetEndpointByID(user1, ep1ID)
	assert.Error(t, err)
}
