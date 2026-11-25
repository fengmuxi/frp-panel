package dao

import (
	"fmt"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"gorm.io/gorm"
)

// ownership.go 资源归属断言（修复安全审计 P0-1）：
//
// PTY / 日志流 / clientsstatus / worker 附加等资源类路由此前只校验"已登录"，
// 未校验"资源属于当前用户"，导致任意租户可以操控其他租户的节点。
// 所有按 ID 操作节点资源的路由，必须在发起 RPC / 返回数据前调用本断言。
//
// 语义：资源 owner 通过；admin 角色放行（运维需要跨租户查看）；其余一律拒绝。

// AssertClientOwnership 校验 client（frpc 节点）归属。
func AssertClientOwnership(ctx *app.Context, userInfo models.UserInfo, clientID string) error {
	if clientID == "" {
		return fmt.Errorf("invalid client id")
	}
	if userInfo != nil && userInfo.IsAdmin() {
		return nil
	}
	if userInfo == nil {
		return fmt.Errorf("client [%s] not accessible: no user info", clientID)
	}
	if _, err := NewQuery(ctx).GetClientByClientID(userInfo, clientID); err != nil {
		return fmt.Errorf("client [%s] not accessible for current user", clientID)
	}
	return nil
}

// AssertServerOwnership 校验 server（frps 节点）归属。
func AssertServerOwnership(ctx *app.Context, userInfo models.UserInfo, serverID string) error {
	if serverID == "" {
		return fmt.Errorf("invalid server id")
	}
	if userInfo != nil && userInfo.IsAdmin() {
		return nil
	}
	if userInfo == nil {
		return fmt.Errorf("server [%s] not accessible: no user info", serverID)
	}
	if _, err := NewQuery(ctx).GetServerByServerID(userInfo, serverID); err != nil {
		return fmt.Errorf("server [%s] not accessible for current user", serverID)
	}
	return nil
}

// AssertProxyOwnership 校验 proxy（隧道）归属，proxyConfigID 为 proxy_config 行主键。
// 与 client/server 断言对称：admin 放行；否则必须能按当前用户查到该行。
func AssertProxyOwnership(ctx *app.Context, userInfo models.UserInfo, proxyConfigID uint) error {
	if proxyConfigID == 0 {
		return fmt.Errorf("invalid proxy config id")
	}
	if userInfo != nil && userInfo.IsAdmin() {
		return nil
	}
	if userInfo == nil {
		return fmt.Errorf("proxy [%d] accessible: no user info", proxyConfigID)
	}
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	var cfg models.ProxyConfig
	// 归属条件与行主键都放同一 struct 条件（struct 查询忽略零值字段，ID 非零生效）
	if err := db.Where(&models.ProxyConfig{
		ProxyConfigEntity: &models.ProxyConfigEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
		Model: &gorm.Model{ID: proxyConfigID},
	}).First(&cfg).Error; err != nil {
		return fmt.Errorf("proxy [%d] not accessible for current user", proxyConfigID)
	}
	return nil
}
