package dao

import (
	"context"
	"fmt"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/rpc"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// 本文件是删除链路的统一级联入口（docs/audit-remediation-plan-2026-11-19.md P1-C）。
// 背景：删除类操作此前只覆盖一半（client 不清 WG/worker 事件、server 不级联、
// /frpc/delete 不清 proxy_config 与通知偏好），形成"半程级联"模式，留下
// 幽灵隧道行、悬挂数据与永不停止的节点侧进程。
//
// 约定：
//   - 入口的归属校验由调用方 handler 完成（DeleteClientHandler/
//     DeleteServerHandler 等先经 userInfo 过滤的资源查询确认归属），
//     本文件按已确认的 ID 执行跨行删除（server 级联会跨用户删除 child）；
//   - 对节点的事件均为尽力而为（后台发送，失败仅记日志——
//     节点侧的进程/接口清理依赖事件，DB 清理不依赖）；
//   - 数据库删除统一在单事务内完成（默认部署无物理外键，显式按 ID 删除）。

// CascadeDeleteClient 删除一个客户端及其全部下游数据（统一级联入口）：
//   - child clients（若传入 origin）或 origin（若传入 child）一并删除；
//   - 全部隧道的 proxy_config 行与通知偏好（按行 ID 清理，避免孤儿）；
//   - 全部流量统计（proxy_stats/tick/hourly/history）；
//   - WireGuard 接口（含 endpoints/links）与节点侧删除事件；
//   - worker 关联（worker_clients）与节点侧 worker 进程删除事件；
//   - clients 行本身（origin + 全部 child，硬删除）。
//
// clientID 可以是 origin 或任意 child 的 ID。
func CascadeDeleteClient(ctx *app.Context, userInfo models.UserInfo, clientID string) error {
	if clientID == "" {
		return fmt.Errorf("invalid client id")
	}
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	q := NewQuery(ctx)
	m := NewMutation(ctx)

	// 1. 解析 client 全集：自身 + 全部 child + origin
	originID := clientID
	if cli, err := q.AdminGetClientByClientID(clientID); err == nil && cli.ClientEntity != nil && cli.OriginClientID != "" {
		originID = cli.OriginClientID
	}
	children, err := q.AdminGetClientsByOriginClientID(originID)
	if err != nil {
		return err
	}
	childrenIDs := lo.Map(children, func(c *models.Client, _ int) string { return c.ClientID })

	allClientIDs := append([]string{originID}, childrenIDs...)

	// 2. 收集下游资源（WG/endpoints）
	wgs, err := q.AdminGetWireGuardsByClientIDs(allClientIDs)
	if err != nil {
		return err
	}
	endpoints, err := q.AdminGetEndpointsByClientIDs(allClientIDs)
	if err != nil {
		return err
	}

	// 3. 尽力下发节点侧清理事件（DB 删除前发送：守护进程仍在线可执行）
	bg := app.NewContext(context.Background(), ctx.GetApp())
	for _, wg := range wgs {
		wgClientID := wg.ClientID
		name := wg.Name
		go func() {
			defer func() { _ = recover() }()
			_, _ = rpc.CallClient(bg, wgClientID, pb.Event_EVENT_DELETE_WIREGUARD, &pb.DeleteWireGuardRequest{
				ClientId:      &wgClientID,
				InterfaceName: &name,
			})
		}()
	}
	for _, child := range children {
		if child.Stopped {
			continue
		}
		childID := child.ClientID
		originDaemon := child.OriginClientID
		go func() {
			defer func() { _ = recover() }()
			_, _ = rpc.CallClient(bg, originDaemon, pb.Event_EVENT_REMOVE_FRPC, &pb.DeleteClientRequest{
				ClientId: &childID,
			})
		}()
		for _, w := range child.Workers {
			workerID := w.ID
			go func() {
				defer func() { _ = recover() }()
				_, _ = rpc.CallClient(bg, originDaemon, pb.Event_EVENT_REMOVE_WORKER, &pb.RemoveWorkerRequest{
					ClientId: &childID,
					WorkerId: &workerID,
				})
			}()
		}
	}

	// 4. DB 级联清理（单事务；默认部署无物理外键，显式按 ID 删除）
	return db.Transaction(func(tx *gorm.DB) error {
		// proxy_config 行：通知偏好按行 ID 清理后删除
		proxyRows := []*models.ProxyConfig{}
		if err := tx.Unscoped().
			Where("client_id IN ? OR origin_client_id IN ?", allClientIDs, allClientIDs).
			Find(&proxyRows).Error; err != nil {
			return err
		}
		for _, row := range proxyRows {
			if err := m.DeleteResourceNotifyPrefs(models.NotifyResTypeProxy, fmt.Sprint(row.ID)); err != nil {
				return err
			}
		}
		if err := tx.Unscoped().
			Where("client_id IN ? OR origin_client_id IN ?", allClientIDs, allClientIDs).
			Delete(&models.ProxyConfig{}).Error; err != nil {
			return err
		}

		// endpoints / wireguard_links
		endpointIDs := lo.Map(endpoints, func(e *models.Endpoint, _ int) uint { return e.ID })
		wgIDs := lo.Map(wgs, func(w *models.WireGuard, _ int) uint { return w.ID })
		if len(wgIDs) > 0 {
			if err := tx.Where("from_wire_guard_id IN ? OR to_wire_guard_id IN ?", wgIDs, wgIDs).
				Delete(&models.WireGuardLink{}).Error; err != nil {
				return err
			}
		}
		if len(endpointIDs) > 0 {
			if err := tx.Where("id IN ?", endpointIDs).Delete(&models.Endpoint{}).Error; err != nil {
				return err
			}
		}

		// 流量统计与快照
		if err := tx.Where("client_id IN ? OR origin_client_id IN ?", allClientIDs, allClientIDs).
			Delete(&models.ProxyStats{}).Error; err != nil {
			return err
		}
		if err := tx.Where("client_id IN ? OR origin_client_id IN ?", allClientIDs, allClientIDs).
			Delete(&models.ProxyStatsTick{}).Error; err != nil {
			return err
		}
		if err := tx.Where("client_id IN ? OR origin_client_id IN ?", allClientIDs, allClientIDs).
			Delete(&models.ProxyStatsHourly{}).Error; err != nil {
			return err
		}
		if err := tx.Where("client_id IN ? OR origin_client_id IN ?", allClientIDs, allClientIDs).
			Delete(&models.HistoryProxyStats{}).Error; err != nil {
			return err
		}

		// worker 关联（join 行）与通知偏好
		for _, id := range allClientIDs {
			if err := tx.Model(&models.Client{ClientEntity: &models.ClientEntity{ClientID: id}}).
				Association("Workers").Clear(); err != nil {
				return err
			}
			if err := m.DeleteResourceNotifyPrefs(models.NotifyResTypeClient, id); err != nil {
				return err
			}
		}

		// clients 行本体（origin + 全部 child，硬删除）
		return tx.Unscoped().
			Where("client_id IN ?", allClientIDs).
			Delete(&models.Client{}).Error
	})
}

// CascadeDeleteServer 删除一个服务端节点及其全部下游数据（统一级联入口）：
// 该节点下的全部 child clients（跨用户——节点删除后这些 child 已失去宿主）、
// 其隧道的 proxy_config 行与通知偏好、流量统计/快照/流水、节点自身的通知偏好，
// 最后删除 servers 行本身。WireGuard 接口随 child 的 client_id 字段清理，
// 并尽力向各归属客户端守护进程下发删除事件。
func CascadeDeleteServer(ctx *app.Context, userInfo models.UserInfo, serverID string) error {
	if serverID == "" {
		return fmt.Errorf("invalid server id")
	}
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	q := NewQuery(ctx)
	m := NewMutation(ctx)

	// 1. 收集该节点下的全部 child clients（跨用户：节点删除后这些 child 失去宿主）
	children, err := q.AdminGetClientsByServerID(serverID)
	if err != nil {
		return err
	}
	childrenIDs := lo.Map(children, func(c *models.Client, _ int) string { return c.ClientID })

	// 2. 尽力下发清理事件：frpc 实例停止 + worker 进程删除 + WG 接口删除
	bg := app.NewContext(context.Background(), ctx.GetApp())
	for _, child := range children {
		if child.Stopped {
			continue
		}
		childID := child.ClientID
		originDaemon := child.OriginClientID
		go func() {
			defer func() { _ = recover() }()
			_, _ = rpc.CallClient(bg, originDaemon, pb.Event_EVENT_REMOVE_FRPC, &pb.DeleteClientRequest{
				ClientId: &childID,
			})
		}()
		for _, w := range child.Workers {
			workerID := w.ID
			go func() {
				defer func() { _ = recover() }()
				_, _ = rpc.CallClient(bg, originDaemon, pb.Event_EVENT_REMOVE_WORKER, &pb.RemoveWorkerRequest{
					ClientId: &childID,
					WorkerId: &workerID,
				})
			}()
		}
	}
	wgs, wgErr := q.AdminGetWireGuardsByClientIDs(childrenIDs)
	if wgErr == nil {
		for _, wg := range wgs {
			wgClientID := wg.ClientID
			name := wg.Name
			go func() {
				defer func() { _ = recover() }()
				_, _ = rpc.CallClient(bg, wgClientID, pb.Event_EVENT_DELETE_WIREGUARD, &pb.DeleteWireGuardRequest{
					ClientId:      &wgClientID,
					InterfaceName: &name,
				})
			}()
		}
	}

	// 3. DB 级联清理（单事务；默认部署无物理外键，显式按 ID 删除）
	return db.Transaction(func(tx *gorm.DB) error {
		// 隧道行：通知偏好按行 ID 清理后删除
		proxyRows := []*models.ProxyConfig{}
		if err := tx.Unscoped().Where("server_id = ?", serverID).Find(&proxyRows).Error; err != nil {
			return err
		}
		for _, row := range proxyRows {
			if err := m.DeleteResourceNotifyPrefs(models.NotifyResTypeProxy, fmt.Sprint(row.ID)); err != nil {
				return err
			}
		}
		if err := tx.Unscoped().Where("server_id = ?", serverID).Delete(&models.ProxyConfig{}).Error; err != nil {
			return err
		}

		// 流量统计与快照
		for _, model := range []interface{}{
			&models.ProxyStats{}, &models.ProxyStatsTick{}, &models.ProxyStatsHourly{}, &models.HistoryProxyStats{},
		} {
			if err := tx.Where("server_id = ?", serverID).Delete(model).Error; err != nil {
				return err
			}
		}

		// child clients 行本体（含各自 ConfigContent；origin 模板保留不删）
		if err := tx.Unscoped().Where("server_id = ?", serverID).Delete(&models.Client{}).Error; err != nil {
			return err
		}

		// 服务端自身行与通知偏好
		if err := m.DeleteResourceNotifyPrefs(models.NotifyResTypeServer, serverID); err != nil {
			return err
		}
		return m.DeleteServer(userInfo, serverID)
	})
}
