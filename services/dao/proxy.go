package dao

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/samber/lo"
	"github.com/sourcegraph/conc/pool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProxyQuery interface {
	GetProxyStatsByClientID(userInfo models.UserInfo, clientID string) ([]*models.ProxyStatsEntity, error)
	GetProxyStatsByServerID(userInfo models.UserInfo, serverID string) ([]*models.ProxyStatsEntity, error)
	GetProxyStatsByUser(userInfo models.UserInfo) ([]*models.ProxyStatsEntity, error)
	AdminGetTenantProxyStats(tenantID int) ([]*models.ProxyStatsEntity, error)
	AdminGetAllProxyStats(tx *gorm.DB) ([]*models.ProxyStatsEntity, error)
	AdminGetProxyConfigByClientIDAndName(clientID string, name string) (*models.ProxyConfig, error)
	GetProxyConfigsByClientID(userInfo models.UserInfo, clientID string) ([]*models.ProxyConfigEntity, error)
	GetProxyConfigByFilter(userInfo models.UserInfo, proxyConfig *models.ProxyConfigEntity) (*models.ProxyConfig, error)
	ListProxyConfigsWithFilters(userInfo models.UserInfo, page, pageSize int, filters *models.ProxyConfigEntity) ([]*models.ProxyConfig, error)
	AdminListProxyConfigsWithFilters(filters *models.ProxyConfigEntity) ([]*models.ProxyConfig, error)
	AdminListProxyConfigsWithRules() ([]*models.ProxyConfig, error)
	AdminGetProxyStatsByKey(userID, tenantID int, serverID, name string) (*models.ProxyStatsEntity, error)
	ListProxyConfigsWithFiltersAndKeyword(userInfo models.UserInfo, page, pageSize int, filters *models.ProxyConfigEntity, keyword string) ([]*models.ProxyConfig, error)
	ListProxyConfigsWithKeyword(userInfo models.UserInfo, page, pageSize int, keyword string) ([]*models.ProxyConfig, error)
	ListProxyConfigs(userInfo models.UserInfo, page, pageSize int) ([]*models.ProxyConfig, error)
	GetProxyConfigByOriginClientIDAndName(userInfo models.UserInfo, clientID string, name string) (*models.ProxyConfig, error)
	CountProxyConfigs(userInfo models.UserInfo) (int64, error)
	CountProxyConfigsWithFilters(userInfo models.UserInfo, filters *models.ProxyConfigEntity) (int64, error)
	CountProxyConfigsWithFiltersAndKeyword(userInfo models.UserInfo, filters *models.ProxyConfigEntity, keyword string) (int64, error)
	GetProxyConfigsByWorkerId(userInfo models.UserInfo, workerID string) ([]*models.ProxyConfig, error)
	GetHourlySnapshotsByUser(userInfo models.UserInfo, fromTs, toTs int64) ([]*models.ProxyStatsHourly, error)
}

type ProxyMutation interface {
	AdminUpdateProxyStats(srv *models.ServerEntity, inputs []*pb.ProxyInfo) error
	AdminCreateProxyConfig(proxyCfg *models.ProxyConfig) error
	RebuildProxyConfigFromClient(userInfo models.UserInfo, client *models.Client) error
	CreateProxyConfig(userInfo models.UserInfo, proxyCfg *models.ProxyConfigEntity) error
	UpdateProxyConfig(userInfo models.UserInfo, proxyCfg *models.ProxyConfig) error
	DeleteProxyConfig(userInfo models.UserInfo, clientID, name string) error
	DeleteProxyConfigsByClientIDOrOriginClientID(userInfo models.UserInfo, clientID string) error
	DeleteProxyConfigsByClientID(userInfo models.UserInfo, clientID string) error
	AdminUpdateProxyConfigAutoStopped(id uint, autoStopped bool) error
	SaveHourlySnapshots(snaps []*models.ProxyStatsHourly) error
	DeleteHourlySnapshotsBefore(beforeTs int64) error
}

type proxyQuery struct{ *queryImpl }
type proxyMutation struct{ *mutationImpl }

func newProxyQuery(base *queryImpl) ProxyQuery          { return &proxyQuery{base} }
func newProxyMutation(base *mutationImpl) ProxyMutation { return &proxyMutation{base} }

func (q *proxyQuery) GetProxyStatsByClientID(userInfo models.UserInfo, clientID string) ([]*models.ProxyStatsEntity, error) {
	if clientID == "" {
		return nil, fmt.Errorf("invalid client id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.ProxyStats{}
	// 用户过滤必须用显式 SQL：struct Where 会把零值字段（UserID=0 的"全局记录"
	// 分支、tenant_id=0 的单租户部署）从条件里丢掉，导致用户过滤静默失效。
	err := db.
		Where("tenant_id = ?", userInfo.GetTenantID()).
		Where("client_id = ? OR origin_client_id = ?", clientID, clientID).
		Where("user_id IN ?", []int{userInfo.GetUserID(), 0}).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return lo.Map(list, func(item *models.ProxyStats, _ int) *models.ProxyStatsEntity {
		return item.ProxyStatsEntity
	}), nil
}

func (q *proxyQuery) GetProxyStatsByServerID(userInfo models.UserInfo, serverID string) ([]*models.ProxyStatsEntity, error) {
	if serverID == "" {
		return nil, fmt.Errorf("invalid server id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.ProxyStats{}
	// 同 GetProxyStatsByClientID：user/tenant 过滤用显式 SQL，避免零值字段被 GORM 丢弃
	err := db.
		Where("tenant_id = ?", userInfo.GetTenantID()).
		Where("server_id = ?", serverID).
		Where("user_id IN ?", []int{userInfo.GetUserID(), 0}).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return lo.Map(list, func(item *models.ProxyStats, _ int) *models.ProxyStatsEntity {
		return item.ProxyStatsEntity
	}), nil
}

// GetProxyStatsByUser 返回当前用户可见的全部实时统计（含 UserID=0 的全局记录），只读无锁
func (q *proxyQuery) GetProxyStatsByUser(userInfo models.UserInfo) ([]*models.ProxyStatsEntity, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.ProxyStats{}
	// 同 GetProxyStatsByClientID：user/tenant 过滤用显式 SQL，避免零值字段被 GORM 丢弃
	err := db.
		Where("tenant_id = ?", userInfo.GetTenantID()).
		Where("user_id IN ?", []int{userInfo.GetUserID(), 0}).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return lo.Map(list, func(item *models.ProxyStats, _ int) *models.ProxyStatsEntity {
		return item.ProxyStatsEntity
	}), nil
}

func (m *proxyMutation) AdminUpdateProxyStats(srv *models.ServerEntity, inputs []*pb.ProxyInfo) error {
	if srv.ServerID == "" {
		return fmt.Errorf("invalid server id")
	}

	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Transaction(func(tx *gorm.DB) error {

		queryResults := make([]interface{}, 3)
		p := pool.New().WithErrors()
		p.Go(
			func() error {
				user := models.User{}
				if err := tx.Where(&models.User{
					UserEntity: &models.UserEntity{
						UserID: srv.UserID,
					},
				}).First(&user).Error; err != nil {
					return err
				}
				queryResults[0] = user
				return nil
			},
		)
		p.Go(
			func() error {
				clients := []*models.Client{}
				if err := tx.
					Where(&models.Client{ClientEntity: &models.ClientEntity{
						UserID:   srv.UserID,
						ServerID: srv.ServerID,
					}}).Find(&clients).Error; err != nil {
					return err
				}
				queryResults[1] = clients
				return nil
			},
		)
		p.Go(
			func() error {
				oldProxy := []*models.ProxyStats{}
				// 按 server_id 加载、不限 user_id：frps 层面 (server_id, 去前缀 name) 唯一，
				// 历史版本/归属变更等原因可能留下 user_id 漂移的旧统计行，按 user_id
				// 过滤会漏掉它们，导致 Save 时主键为空直接插入新行（同名重复统计且永不收敛）。
				if err := tx.
					Where("server_id = ?", srv.ServerID).Find(&oldProxy).Error; err != nil {
					return err
				}
				queryResults[2] = oldProxy
				return nil
			},
		)
		if err := p.Wait(); err != nil {
			return err
		}

		user := queryResults[0].(models.User)
		clients := queryResults[1].([]*models.Client)
		oldProxyRows := queryResults[2].([]*models.ProxyStats)

		inputMap := map[string]*pb.ProxyInfo{}
		proxyMap := map[string]*models.ProxyStatsEntity{}
		for _, proxyInfo := range inputs {
			if proxyInfo == nil {
				continue
			}
			proxyName := strings.TrimPrefix(proxyInfo.GetName(), user.UserName+".")
			proxyMap[proxyName] = &models.ProxyStatsEntity{
				ServerID:        srv.ServerID,
				Name:            proxyName,
				Type:            proxyInfo.GetType(),
				UserID:          srv.UserID,
				TenantID:        srv.TenantID,
				TodayTrafficIn:  proxyInfo.GetTodayTrafficIn(),
				TodayTrafficOut: proxyInfo.GetTodayTrafficOut(),
			}
			inputMap[proxyName] = proxyInfo
		}

		// 同 (server, name) 重复统计行收敛：只处理本次上报涉及的隧道名
		//（frps 重启期间隧道可能暂时缺席，缺席的名字不动，避免误删）。
		// 优先保留当前归属者的行（否则取最近更新的行），其余行的历史累计
		// 并入保留行后软删除，让漂移产生的重复行在下次上报时自动收敛。
		oldProxyByName := map[string][]*models.ProxyStats{}
		for _, p := range oldProxyRows {
			oldProxyByName[p.Name] = append(oldProxyByName[p.Name], p)
		}
		oldProxyMap := make(map[string]*models.ProxyStats, len(proxyMap))
		for name := range proxyMap {
			rows := oldProxyByName[name]
			if len(rows) == 0 {
				continue
			}
			pick := pickKeptStatsRow(rows, srv.UserID)
			for _, r := range rows {
				if r.ProxyID == pick.ProxyID {
					continue
				}
				pick.HistoryTrafficIn += r.HistoryTrafficIn
				pick.HistoryTrafficOut += r.HistoryTrafficOut
				if err := tx.Delete(r).Error; err != nil {
					return err
				}
			}
			oldProxyMap[name] = pick
		}

		proxyEntityMap := map[string]*models.ProxyStatsEntity{}
		for _, client := range clients {
			cliCfg, err := client.GetConfigContent()
			if err != nil || cliCfg == nil {
				continue
			}
			for _, cfg := range cliCfg.Proxies {
				if proxy, ok := proxyMap[cfg.GetBaseConfig().Name]; ok {
					proxy.ClientID = client.ClientID
					proxy.OriginClientID = client.OriginClientID
					proxyEntityMap[proxy.Name] = proxy
				}
			}
		}

		nowTime := time.Now()
		results := lo.Values(lo.MapValues(proxyEntityMap, func(p *models.ProxyStatsEntity, name string) *models.ProxyStats {
			item := &models.ProxyStats{
				ProxyStatsEntity: p,
			}
			if oldProxy, ok := oldProxyMap[name]; ok {
				item.ProxyID = oldProxy.ProxyID

				// 今日/历史累计的补偿维护（跨 frps 重启连续）：
				//   - 正常推进时按 frps 内存计数的增量累加今日值；
				//   - 检测到上报回退（frps 重启/firstSync）时衔接今日累计，避免丢流量；
				//   - 跨天时昨日今日累计滚入历史。
				// 注意：firstSync（frps 重启后的首次同步）不再把旧今日值滚入历史——
				// history 的语义是"今天 00:00 的累计"，滚入会让区间差分虚高。
				firstSync := inputMap[name].GetFirstSync()
				upd := computeProxyStatsDailyUpdate(
					oldProxy.TodayTrafficIn, oldProxy.TodayTrafficOut,
					oldProxy.HistoryTrafficIn, oldProxy.HistoryTrafficOut,
					oldProxy.ReportedTodayIn, oldProxy.ReportedTodayOut,
					inputMap[name].GetTodayTrafficIn(), inputMap[name].GetTodayTrafficOut(),
					firstSync,
					utils.IsSameDay(nowTime, oldProxy.UpdatedAt),
				)
				item.TodayTrafficIn = upd.TodayIn
				item.TodayTrafficOut = upd.TodayOut
				item.HistoryTrafficIn = upd.HistoryIn
				item.HistoryTrafficOut = upd.HistoryOut
				item.ReportedTodayIn = lo.ToPtr(upd.ReportedIn)
				item.ReportedTodayOut = lo.ToPtr(upd.ReportedOut)
			}
			return item
		}))

		if len(results) > 0 {
			return tx.Save(results).Error
		}
		return nil
	})
}

// pickKeptStatsRow 从同一 (server_id, name) 的多条统计行中选出保留行：
// 优先保留当前节点归属者的行（归属漂移产生的孤儿行会被软删除），
// 同为归属者/同为非归属者时取最近更新的行。
func pickKeptStatsRow(rows []*models.ProxyStats, srvUserID int) *models.ProxyStats {
	pick := rows[0]
	for _, r := range rows[1:] {
		if (r.UserID == srvUserID) != (pick.UserID == srvUserID) {
			if r.UserID == srvUserID {
				pick = r
			}
			continue
		}
		if r.UpdatedAt.After(pick.UpdatedAt) {
			pick = r
		}
	}
	return pick
}

func (q *proxyQuery) AdminGetTenantProxyStats(tenantID int) ([]*models.ProxyStatsEntity, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.ProxyStats{}
	err := db.
		Where(&models.ProxyStats{ProxyStatsEntity: &models.ProxyStatsEntity{
			TenantID: tenantID,
		}}).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return lo.Map(list, func(item *models.ProxyStats, _ int) *models.ProxyStatsEntity {
		return item.ProxyStatsEntity
	}), nil
}

func (q *proxyQuery) AdminGetAllProxyStats(tx *gorm.DB) ([]*models.ProxyStatsEntity, error) {
	db := tx
	list := []*models.ProxyStats{}
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return lo.Map(list, func(item *models.ProxyStats, _ int) *models.ProxyStatsEntity {
		return item.ProxyStatsEntity
	}), nil
}

func (m *proxyMutation) AdminCreateProxyConfig(proxyCfg *models.ProxyConfig) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Create(proxyCfg).Error
}

// RebuildProxyConfigFromClient rebuild proxy from client
// skip stopped proxy
func (m *proxyMutation) RebuildProxyConfigFromClient(userInfo models.UserInfo, client *models.Client) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	query := NewQuery(m.ctx)

	pxyCfgs, err := utils.LoadProxiesFromContent(client.ConfigContent)
	if err != nil {
		return err
	}

	proxyConfigEntities := []*models.ProxyConfig{}

	for _, pxyCfg := range pxyCfgs {
		proxyCfg := &models.ProxyConfig{
			ProxyConfigEntity: &models.ProxyConfigEntity{},
		}
		if oldProxyCfg, err := query.GetProxyConfigByOriginClientIDAndName(userInfo, client.ClientID, pxyCfg.GetBaseConfig().Name); err == nil {
			logger.Logger(context.Background()).WithError(err).Warnf("proxy config already exist, will be override, clientID: [%s], name: [%s]",
				client.ClientID, pxyCfg.GetBaseConfig().Name)
			proxyCfg.Model = oldProxyCfg.Model
		}

		if err := proxyCfg.FillClientConfig(client.ClientEntity); err != nil {
			return err
		}

		if err := proxyCfg.FillTypedProxyConfig(pxyCfg); err != nil {
			return err
		}

		proxyConfigEntities = append(proxyConfigEntities, proxyCfg)
	}

	if err := m.DeleteProxyConfigsByClientIDOrOriginClientID(userInfo, client.ClientID); err != nil {
		return err
	}

	if len(proxyConfigEntities) == 0 {
		return nil
	}

	return db.Save(proxyConfigEntities).Error
}

func (q *proxyQuery) AdminGetProxyConfigByClientIDAndName(clientID string, name string) (*models.ProxyConfig, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	proxyCfg := &models.ProxyConfig{}
	err := db.
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
			ClientID: clientID,
			Name:     name,
		}}).
		First(proxyCfg).Error
	if err != nil {
		return nil, err
	}
	return proxyCfg, nil
}

func (q *proxyQuery) GetProxyConfigsByClientID(userInfo models.UserInfo, clientID string) ([]*models.ProxyConfigEntity, error) {
	if clientID == "" {
		return nil, fmt.Errorf("invalid client id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.ProxyConfig{}
	err := db.
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
			ClientID: clientID,
		}}).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return lo.Map(list, func(item *models.ProxyConfig, _ int) *models.ProxyConfigEntity {
		return item.ProxyConfigEntity
	}), nil
}

func (q *proxyQuery) GetProxyConfigByFilter(userInfo models.UserInfo, proxyConfig *models.ProxyConfigEntity) (*models.ProxyConfig, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	filter := &models.ProxyConfigEntity{}

	if len(proxyConfig.ClientID) != 0 {
		filter.ClientID = proxyConfig.ClientID
	}
	if len(proxyConfig.OriginClientID) != 0 {
		filter.OriginClientID = proxyConfig.OriginClientID
	}
	if len(proxyConfig.Name) != 0 {
		filter.Name = proxyConfig.Name
	}
	if len(proxyConfig.Type) != 0 {
		filter.Type = proxyConfig.Type
	}
	if len(proxyConfig.ServerID) != 0 {
		filter.ServerID = proxyConfig.ServerID
	}

	filter.UserID = userInfo.GetUserID()
	filter.TenantID = userInfo.GetTenantID()

	respProxyCfg := &models.ProxyConfig{}
	err := db.
		Where(&models.ProxyConfig{ProxyConfigEntity: filter}).
		First(respProxyCfg).Error
	if err != nil {
		return nil, err
	}
	return respProxyCfg, nil
}

func (q *proxyQuery) ListProxyConfigsWithFilters(userInfo models.UserInfo, page, pageSize int, filters *models.ProxyConfigEntity) ([]*models.ProxyConfig, error) {
	if page < 1 || pageSize < 1 {
		return nil, fmt.Errorf("invalid page or page size")
	}

	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	offset := (page - 1) * pageSize

	filters.UserID = userInfo.GetUserID()
	filters.TenantID = userInfo.GetTenantID()

	var proxyConfigs []*models.ProxyConfig
	err := db.Where(&models.ProxyConfig{
		ProxyConfigEntity: filters,
	}).Where(filters).Offset(offset).Limit(pageSize).Find(&proxyConfigs).Error
	if err != nil {
		return nil, err
	}

	return proxyConfigs, nil
}

func (q *proxyQuery) AdminListProxyConfigsWithFilters(filters *models.ProxyConfigEntity) ([]*models.ProxyConfig, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()

	var proxyConfigs []*models.ProxyConfig
	err := db.Where(&models.ProxyConfig{
		ProxyConfigEntity: filters,
	}).Where(filters).Find(&proxyConfigs).Error
	if err != nil {
		return nil, err
	}

	return proxyConfigs, nil
}

// AdminListProxyConfigsWithRules 返回启用了自动规则（定时开关/流量限额）或曾被自动停用的隧道，
// 供 Master 周期对账任务评估期望启停状态。
func (q *proxyQuery) AdminListProxyConfigsWithRules() ([]*models.ProxyConfig, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()

	var proxyConfigs []*models.ProxyConfig
	err := db.Where(
		db.Where("schedule_enabled = ?", true).
			Or("auto_stopped = ?", true).
			Or(db.Where("quota_type IS NOT NULL").Where("quota_type <> ?", "")),
	).Find(&proxyConfigs).Error
	if err != nil {
		return nil, err
	}

	return proxyConfigs, nil
}

// AdminGetProxyStatsByKey 按 用户/租户/服务端/隧道名 精确查找流量统计行（内部规则计算用，无需请求级用户上下文）
func (q *proxyQuery) AdminGetProxyStatsByKey(userID, tenantID int, serverID, name string) (*models.ProxyStatsEntity, error) {
	if serverID == "" || name == "" {
		return nil, nil
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()

	var list []*models.ProxyStats
	err := db.Where(&models.ProxyStats{ProxyStatsEntity: &models.ProxyStatsEntity{
		UserID:   userID,
		TenantID: tenantID,
		ServerID: serverID,
		Name:     name,
	}}).Find(&list).Error
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0].ProxyStatsEntity, nil
}

func (q *proxyQuery) ListProxyConfigsWithFiltersAndKeyword(userInfo models.UserInfo, page, pageSize int, filters *models.ProxyConfigEntity, keyword string) ([]*models.ProxyConfig, error) {
	if page < 1 || pageSize < 1 || len(keyword) == 0 {
		return nil, fmt.Errorf("invalid page or page size or keyword")
	}

	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	offset := (page - 1) * pageSize

	filters.UserID = userInfo.GetUserID()
	filters.TenantID = userInfo.GetTenantID()

	var proxyConfigs []*models.ProxyConfig
	err := db.Where(&models.ProxyConfig{
		ProxyConfigEntity: filters,
	}).Where(filters).Where("name like ?", "%"+keyword+"%").Offset(offset).Limit(pageSize).Find(&proxyConfigs).Error
	if err != nil {
		return nil, err
	}

	return proxyConfigs, nil
}

func (q *proxyQuery) ListProxyConfigsWithKeyword(userInfo models.UserInfo, page, pageSize int, keyword string) ([]*models.ProxyConfig, error) {
	return q.ListProxyConfigsWithFiltersAndKeyword(userInfo, page, pageSize, &models.ProxyConfigEntity{}, keyword)
}

func (q *proxyQuery) ListProxyConfigs(userInfo models.UserInfo, page, pageSize int) ([]*models.ProxyConfig, error) {
	return q.ListProxyConfigsWithFilters(userInfo, page, pageSize, &models.ProxyConfigEntity{})
}

func (m *proxyMutation) CreateProxyConfig(userInfo models.UserInfo, proxyCfg *models.ProxyConfigEntity) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	proxyCfg.UserID = userInfo.GetUserID()
	proxyCfg.TenantID = userInfo.GetTenantID()
	return db.Create(&models.ProxyConfig{ProxyConfigEntity: proxyCfg}).Error
}

func (m *proxyMutation) UpdateProxyConfig(userInfo models.UserInfo, proxyCfg *models.ProxyConfig) error {
	if proxyCfg.ID == 0 {
		return fmt.Errorf("invalid proxy config id")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	proxyCfg.UserID = userInfo.GetUserID()
	proxyCfg.TenantID = userInfo.GetTenantID()
	return db.Where(&models.ProxyConfig{
		ProxyConfigEntity: &models.ProxyConfigEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
			ClientID: proxyCfg.ClientID,
		},
		Model: &gorm.Model{
			ID: proxyCfg.ID,
		},
	}).Save(proxyCfg).Error
}

// AdminUpdateProxyConfigAutoStopped 仅更新 auto_stopped 标记（对账任务用，按主键直更，不做用户过滤）
func (m *proxyMutation) AdminUpdateProxyConfigAutoStopped(id uint, autoStopped bool) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Model(&models.ProxyConfig{}).Where("id = ?", id).Update("auto_stopped", autoStopped).Error
}

func (m *proxyMutation) DeleteProxyConfig(userInfo models.UserInfo, clientID, name string) error {
	if clientID == "" || name == "" {
		return fmt.Errorf("invalid client id or name")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Unscoped().
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
			ClientID: clientID,
			Name:     name,
		}}).
		Delete(&models.ProxyConfig{}).Error
}

func (m *proxyMutation) DeleteProxyConfigsByClientIDOrOriginClientID(userInfo models.UserInfo, clientID string) error {
	if clientID == "" {
		return fmt.Errorf("invalid client id")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Unscoped().
		Where(
			db.Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
				ClientID: clientID,
			}}).
				Or(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
					UserID:         userInfo.GetUserID(),
					TenantID:       userInfo.GetTenantID(),
					OriginClientID: clientID,
				}})).
		Where(db.Where("stopped is NULL").
			Or("stopped = ?", false)).
		Delete(&models.ProxyConfig{}).Error
}

func (m *proxyMutation) DeleteProxyConfigsByClientID(userInfo models.UserInfo, clientID string) error {
	if clientID == "" {
		return fmt.Errorf("invalid client id")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Unscoped().
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
			ClientID: clientID,
		}}).
		Delete(&models.ProxyConfig{}).Error
}

func (q *proxyQuery) GetProxyConfigByOriginClientIDAndName(userInfo models.UserInfo, clientID string, name string) (*models.ProxyConfig, error) {
	if clientID == "" || name == "" {
		return nil, fmt.Errorf("invalid client id or name")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	item := &models.ProxyConfig{}
	err := db.
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
			UserID:         userInfo.GetUserID(),
			TenantID:       userInfo.GetTenantID(),
			OriginClientID: clientID,
			Name:           name,
		}}).
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (q *proxyQuery) CountProxyConfigs(userInfo models.UserInfo) (int64, error) {
	return q.CountProxyConfigsWithFilters(userInfo, &models.ProxyConfigEntity{})
}

func (q *proxyQuery) CountProxyConfigsWithFilters(userInfo models.UserInfo, filters *models.ProxyConfigEntity) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	filters.UserID = userInfo.GetUserID()
	filters.TenantID = userInfo.GetTenantID()

	var count int64
	err := db.Model(&models.ProxyConfig{}).Where(&models.ProxyConfig{
		ProxyConfigEntity: filters,
	}).Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (q *proxyQuery) CountProxyConfigsWithFiltersAndKeyword(userInfo models.UserInfo, filters *models.ProxyConfigEntity, keyword string) (int64, error) {
	if len(keyword) == 0 {
		return q.CountProxyConfigsWithFilters(userInfo, filters)
	}

	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	filters.UserID = userInfo.GetUserID()
	filters.TenantID = userInfo.GetTenantID()

	var count int64
	err := db.Model(&models.ProxyConfig{}).Where(&models.ProxyConfig{
		ProxyConfigEntity: filters,
	}).Where("name like ?", "%"+keyword+"%").Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (q *proxyQuery) GetProxyConfigsByWorkerId(userInfo models.UserInfo, workerID string) ([]*models.ProxyConfig, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	items := []*models.ProxyConfig{}

	err := db.
		Where(&models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
			WorkerID: workerID,
		}).
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}
