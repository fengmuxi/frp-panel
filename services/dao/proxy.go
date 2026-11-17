package dao

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/samber/lo"
	"github.com/sourcegraph/conc/pool"
	"gorm.io/gorm"
)

type ProxyQuery interface {
	GetProxyStatsByClientID(userInfo models.UserInfo, clientID string) ([]*models.ProxyStatsEntity, error)
	GetProxyStatsByServerID(userInfo models.UserInfo, serverID string) ([]*models.ProxyStatsEntity, error)
	AdminGetProxyStatsByServerID(serverID string) ([]*models.ProxyStatsEntity, error)
	AdminGetProxyStatTickBucketsInRange(userInfo models.UserInfo, from, to int64, bucket int64) ([]*ProxyStatsTickBucket, error)
	GetProxyStatsByUser(userInfo models.UserInfo) ([]*models.ProxyStatsEntity, error)
	AdminGetTenantProxyStats(tenantID int) ([]*models.ProxyStatsEntity, error)
	AdminGetAllProxyStats(tx *gorm.DB) ([]*models.ProxyStatsEntity, error)
	AdminGetProxyConfigByClientIDAndName(clientID string, name string) (*models.ProxyConfig, error)
	AdminGetProxyConfigByID(id uint) (*models.ProxyConfig, error)
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
	AdminSaveProxyStatTicks(tx *gorm.DB, ticks []*models.ProxyStatsTick) error
	AdminDeleteProxyStatTicksBefore(beforeTs int64) error
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

// AdminGetProxyStatsByServerID 服务端内部使用：按节点取全部实时统计（不限用户）。
// 统计行归属为各隧道的 client 归属用户，节点维度的内部逻辑（掉线检测等）必须
// 用本方法而非带用户过滤的 GetProxyStatsByServerID，否则会漏掉其他用户的隧道。
func (q *proxyQuery) AdminGetProxyStatsByServerID(serverID string) ([]*models.ProxyStatsEntity, error) {
	if serverID == "" {
		return nil, fmt.Errorf("invalid server id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.ProxyStats{}
	err := db.Where("server_id = ?", serverID).Find(&list).Error
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
				// 不限 user_id：多租户下其他用户的 client 也可连接本节点，
				// 其隧道的流量统计需要归属到各自 client（而非节点 owner）
				if err := tx.
					Where("server_id = ?", srv.ServerID).Find(&clients).Error; err != nil {
					return err
				}
				queryResults[1] = clients
				return nil
			},
		)
		p.Go(
			func() error {
				oldProxy := []*models.ProxyStats{}
				// 按 server_id 加载、不限 user_id：frps 层面 (server_id, frps 全名) 唯一，
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

		// client owner 用户名映射：frps multiuser 插件给隧道名加 "userName." 前缀，
		// 需按 client 所属用户去前缀才能与 client 配置里的裸隧道名对上
		userNames := map[int]string{srv.UserID: user.UserName}
		knownUserIDs := []int{srv.UserID}
		for _, client := range clients {
			if client.UserID != srv.UserID {
				knownUserIDs = append(knownUserIDs, client.UserID)
			}
		}
		if len(knownUserIDs) > 1 {
			users := []*models.User{}
			if err := tx.Where("user_id IN ?", knownUserIDs).Find(&users).Error; err != nil {
				return err
			}
			for _, u := range users {
				userNames[u.UserID] = u.UserName
			}
		}

		// 上报项按 (去前缀裸名, 前缀归属用户) 索引：不同用户的同名隧道在 frps
		// 层面靠 user 前缀区分，去前缀后必须连同归属用户一起作为匹配键；
		// 无前缀（multiuser 插件未启用）时归属用户记 0，供任意 client 回退匹配
		inputByKey := make(map[proxyStatsInputKey]*pb.ProxyInfo, len(inputs))
		for _, proxyInfo := range inputs {
			if proxyInfo == nil || strings.TrimSpace(proxyInfo.GetName()) == "" {
				continue
			}
			stripped, ownerID := stripProxyStatsPrefix(strings.TrimSpace(proxyInfo.GetName()), userNames)
			inputByKey[proxyStatsInputKey{name: stripped, userID: ownerID}] = proxyInfo
		}

		// 与 client 配置匹配：owner 前缀命中的上报项优先，其次无前缀上报项；
		// 同一上报项可能同时命中 origin 与 shadow client 的配置，后遍历的 client
		// 覆盖 ClientID 归属（shadow 是实际连接本节点的实例）
		proxyEntityMap := make(map[proxyStatsInputKey]*proxyStatsMatch, len(inputByKey))
		for _, client := range clients {
			cliCfg, err := client.GetConfigContent()
			if err != nil || cliCfg == nil {
				continue
			}
			for _, cfg := range cliCfg.Proxies {
				cfgName := cfg.GetBaseConfig().Name
				info, ok := inputByKey[proxyStatsInputKey{name: cfgName, userID: client.UserID}]
				if !ok {
					info, ok = inputByKey[proxyStatsInputKey{name: cfgName, userID: 0}]
				}
				if !ok {
					continue
				}
				proxyEntityMap[proxyStatsInputKey{name: cfgName, userID: client.UserID}] = &proxyStatsMatch{
					entity: &models.ProxyStatsEntity{
						ServerID:        srv.ServerID,
						Name:            cfgName,
						Type:            info.GetType(),
						ClientID:        client.ClientID,
						OriginClientID:  client.OriginClientID,
						UserID:          client.UserID,
						TenantID:        client.TenantID,
						TodayTrafficIn:  info.GetTodayTrafficIn(),
						TodayTrafficOut: info.GetTodayTrafficOut(),
						StatsDate:       info.GetStatsDate(),
					},
					info: info,
				}
			}
		}

		// 旧统计行按裸名分桶，逐匹配项收敛：
		//   - 同 (裸名, ClientID) 的多行是同一隧道的重复行（历史版本/归属漂移产生），
		//     保留最近更新的一行，其余行的历史累计并入后软删除；
		//   - ClientID 已不在本节点活跃 client 集合中的行（client 被删除重建）视同
		//     孤儿，仅并入同归属用户的行，避免把他人历史流量记错归属；
		//   - 不同活跃 client 的同名行是不同用户的隧道，各自保留。
		// 仅处理本次上报命中的隧道名：frps 重启期间缺席的隧道不动，避免误删。
		activeClientIDs := make(map[string]struct{}, len(clients))
		for _, client := range clients {
			activeClientIDs[client.ClientID] = struct{}{}
		}
		oldProxyByName := map[string][]*models.ProxyStats{}
		for _, r := range oldProxyRows {
			oldProxyByName[r.Name] = append(oldProxyByName[r.Name], r)
		}
		oldProxyMap := make(map[proxyStatsInputKey]*models.ProxyStats, len(proxyEntityMap))
		for key, matched := range proxyEntityMap {
			var pool []*models.ProxyStats
			for _, r := range oldProxyByName[key.name] {
				if _, active := activeClientIDs[r.ClientID]; r.ClientID == matched.entity.ClientID ||
					(!active && r.UserID == matched.entity.UserID) {
					pool = append(pool, r)
				}
			}
			if len(pool) == 0 {
				continue
			}
			pick := pickKeptStatsRow(pool)
			for _, r := range pool {
				if r.ProxyID == pick.ProxyID {
					continue
				}
				pick.HistoryTrafficIn += r.HistoryTrafficIn
				pick.HistoryTrafficOut += r.HistoryTrafficOut
				if err := tx.Delete(r).Error; err != nil {
					return err
				}
			}
			oldProxyMap[key] = pick
		}

		nowTime := time.Now()
		results := make([]*models.ProxyStats, 0, len(proxyEntityMap))
		ticks := make([]*models.ProxyStatsTick, 0, len(proxyEntityMap))
		for key, matched := range proxyEntityMap {
			item := &models.ProxyStats{ProxyStatsEntity: matched.entity}
			if oldProxy, ok := oldProxyMap[key]; ok {
				item.ProxyID = oldProxy.ProxyID

				// 今日/历史累计的补偿维护（跨 frps 重启连续）：
				//   - 正常推进时按 frps 内存计数的增量累加今日值；
				//   - 检测到上报回退（frps 重启/firstSync）时衔接今日累计，避免丢流量；
				//   - 跨天时昨日今日累计滚入历史。
				// 注意：firstSync（frps 重启后的首次同步）不再把旧今日值滚入历史——
				// history 的语义是"今天 00:00 的累计"，滚入会让区间差分虚高。
				// 跨天判断优先用上报携带的 frps 本地日期（节点时区与 master 不一致、
				// 推送延迟跨零点时仍正确）；旧节点未上报日期时回退本地日判断。
				inputDate := matched.info.GetStatsDate()
				isSameDay := utils.IsSameDay(nowTime, oldProxy.UpdatedAt)
				if inputDate != "" && oldProxy.StatsDate != "" {
					isSameDay = inputDate == oldProxy.StatsDate
				}
				upd := computeProxyStatsDailyUpdate(
					oldProxy.TodayTrafficIn, oldProxy.TodayTrafficOut,
					oldProxy.HistoryTrafficIn, oldProxy.HistoryTrafficOut,
					oldProxy.ReportedTodayIn, oldProxy.ReportedTodayOut,
					matched.info.GetTodayTrafficIn(), matched.info.GetTodayTrafficOut(),
					matched.info.GetFirstSync(),
					isSameDay,
				)
				item.TodayTrafficIn = upd.TodayIn
				item.TodayTrafficOut = upd.TodayOut
				item.HistoryTrafficIn = upd.HistoryIn
				item.HistoryTrafficOut = upd.HistoryOut
				item.ReportedTodayIn = lo.ToPtr(upd.ReportedIn)
				item.ReportedTodayOut = lo.ToPtr(upd.ReportedOut)
				if inputDate != "" {
					item.StatsDate = inputDate
				}

				// 本推送周期的流量增量落流水（分钟粒度趋势的数据源）。
				// 跨天翻转时增量可能为负（昨日累计被今日小值替代），钳 0 后丢弃；
				// frps 重启衔接时增量即断网期间流量，跳变正常。
				deltaIn := item.TodayTrafficIn - oldProxy.TodayTrafficIn
				deltaOut := item.TodayTrafficOut - oldProxy.TodayTrafficOut
				if deltaIn > 0 || deltaOut > 0 {
					if deltaIn < 0 {
						deltaIn = 0
					}
					if deltaOut < 0 {
						deltaOut = 0
					}
					ticks = append(ticks, &models.ProxyStatsTick{
						ProxyID:  item.ProxyID,
						Ts:       nowTime.Unix(),
						DeltaIn:  deltaIn,
						DeltaOut: deltaOut,
						Name:     item.Name,
						Type:     item.Type,
						ServerID: item.ServerID,
						ClientID: item.ClientID,
						UserID:   item.UserID,
						TenantID: item.TenantID,
					})
				}
			}
			results = append(results, item)
		}

		if len(results) > 0 {
			if err := tx.Save(results).Error; err != nil {
				return err
			}
		}
		return m.AdminSaveProxyStatTicks(tx, ticks)
	})
}

// proxyStatsInputKey 是统计上报项与统计行的匹配键：去前缀后的裸隧道名 +
// 前缀归属用户（0 表示无用户前缀的上报，可被任意 client 回退匹配）。
type proxyStatsInputKey struct {
	name   string
	userID int
}

// proxyStatsMatch 是一次上报与 client 配置的匹配结果。
type proxyStatsMatch struct {
	entity *models.ProxyStatsEntity
	info   *pb.ProxyInfo
}

// stripProxyStatsPrefix 去掉 frps multiuser 添加的 "userName." 前缀，
// 返回 (裸隧道名, 前缀归属用户 ID)；无已知用户前缀时返回 (原名, 0)。
// 用户名按长度降序尝试，避免短前缀误吞（如用户名 "a" 与 "ab" 并存时，
// "ab.emby" 必须按 "ab" 去前缀）。
func stripProxyStatsPrefix(name string, userNames map[int]string) (string, int) {
	type idPrefix struct {
		id     int
		prefix string
	}
	all := make([]idPrefix, 0, len(userNames))
	for id, prefix := range userNames {
		if prefix != "" && strings.HasPrefix(name, prefix+".") {
			all = append(all, idPrefix{id: id, prefix: prefix})
		}
	}
	if len(all) == 0 {
		return name, 0
	}
	sort.Slice(all, func(i, j int) bool { return len(all[i].prefix) > len(all[j].prefix) })
	return strings.TrimPrefix(name, all[0].prefix+"."), all[0].id
}

// pickKeptStatsRow 从同一隧道的多条统计行中选出保留行：取最近更新的行
// （孤儿行因不再接收推送必然更旧，自然落选）。
func pickKeptStatsRow(rows []*models.ProxyStats) *models.ProxyStats {
	pick := rows[0]
	for _, r := range rows[1:] {
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

// AdminGetAllProxyStats 全量读取实时统计（供每日/每小时快照任务使用）。
// 纯读操作，不加 FOR UPDATE：SQLite 不支持该语法且裸连接上的锁无意义。
func (q *proxyQuery) AdminGetAllProxyStats(tx *gorm.DB) ([]*models.ProxyStatsEntity, error) {
	db := tx
	list := []*models.ProxyStats{}
	err := db.Find(&list).Error
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

// AdminGetProxyConfigByID 按行主键读取隧道配置（admin 语义，不校验归属）
func (q *proxyQuery) AdminGetProxyConfigByID(id uint) (*models.ProxyConfig, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	proxyCfg := &models.ProxyConfig{}
	if err := db.First(proxyCfg, id).Error; err != nil {
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
	filter := &models.ProxyConfig{ProxyConfigEntity: &models.ProxyConfigEntity{
		UserID:   userInfo.GetUserID(),
		TenantID: userInfo.GetTenantID(),
		ClientID: clientID,
		Name:     name,
	}}

	// 先取行主键（通知覆盖配置按 proxy_config.ID 关联），用于删除后级联清理
	proxyConfigID := ""
	var cfg models.ProxyConfig
	if err := db.Unscoped().Where(filter).First(&cfg).Error; err == nil && cfg.ID != 0 {
		proxyConfigID = fmt.Sprint(cfg.ID)
	}

	if err := db.Unscoped().Where(filter).Delete(&models.ProxyConfig{}).Error; err != nil {
		return err
	}
	if proxyConfigID != "" {
		return deleteResourceNotifyPrefs(m.ctx, models.NotifyResTypeProxy, proxyConfigID)
	}
	return nil
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
