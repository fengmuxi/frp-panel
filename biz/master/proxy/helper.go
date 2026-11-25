package proxy

import (
	"errors"

	"github.com/fengmuxi/frp-panel/biz/master/client"
	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

func convertProxyStatsList(proxyList []*models.ProxyStatsEntity) []*pb.ProxyInfo {
	return lo.Map(proxyList, func(item *models.ProxyStatsEntity, index int) *pb.ProxyInfo {
		return &pb.ProxyInfo{
			Name:              lo.ToPtr(item.Name),
			Type:              lo.ToPtr(item.Type),
			ClientId:          lo.ToPtr(item.ClientID),
			ServerId:          lo.ToPtr(item.ServerID),
			TodayTrafficIn:    lo.ToPtr(item.TodayTrafficIn),
			TodayTrafficOut:   lo.ToPtr(item.TodayTrafficOut),
			HistoryTrafficIn:  lo.ToPtr(item.HistoryTrafficIn),
			HistoryTrafficOut: lo.ToPtr(item.HistoryTrafficOut),
		}
	})
}

// fillProxyRulesInfo 填充隧道自动规则字段与当前统计窗口已用量（list/get 返回时共用）
func fillProxyRulesInfo(ctx *app.Context, pbCfg *pb.ProxyConfig, item *models.ProxyConfig) {
	pbCfg.ScheduleEnabled = lo.ToPtr(item.ScheduleEnabled)
	pbCfg.ScheduleStart = lo.ToPtr(item.ScheduleStart)
	pbCfg.ScheduleEnd = lo.ToPtr(item.ScheduleEnd)
	pbCfg.ScheduleDays = lo.Map(item.ScheduleDays, func(day int, _ int) int32 { return int32(day) })
	pbCfg.QuotaType = lo.ToPtr(item.QuotaType)
	pbCfg.QuotaLimit = lo.ToPtr(item.QuotaLimit)
	pbCfg.QuotaDirection = lo.ToPtr(item.QuotaDirection)
	pbCfg.AutoStopped = lo.ToPtr(item.AutoStopped)
	pbCfg.QuotaUsed = lo.ToPtr(CalcProxyQuotaUsed(ctx, item.ProxyConfigEntity))
}

// GetClientWithMakeShadow
// 1. 检查是否有已连接该服务端的客户端
// 2. 检查是否有Shadow客户端
// 3. 如果没有，则新建Shadow客户端和子客户端
func GetClientWithMakeShadow(c *app.Context, clientID, serverID string) (*models.ClientEntity, error) {
	userInfo := common.GetUserInfo(c)
	clientEntity, err := dao.NewQuery(c).GetClientByFilter(userInfo, &models.ClientEntity{OriginClientID: clientID, ServerID: serverID}, lo.ToPtr(false))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		clientEntity, err = dao.NewQuery(c).GetClientByFilter(userInfo, &models.ClientEntity{ClientID: clientID}, nil)
		if err != nil {
			logger.Logger(c).WithError(err).Errorf("cannot get client, id: [%s]", clientID)
			return nil, err
		}
		if (!clientEntity.IsShadow || len(clientEntity.ConfigContent) != 0) && len(clientEntity.OriginClientID) == 0 {
			// 没shadow过，需要shadow
			_, err = client.MakeClientShadowed(c, serverID, clientEntity)
			if err != nil {
				logger.Logger(c).WithError(err).Errorf("cannot make client shadow, id: [%s]", clientID)
				return nil, err
			}
		}
		// shadow过，但没找到子客户端，需要新建
		clientEntity, _, err = client.ChildClientForServer(c, serverID, clientEntity)
		if err != nil {
			logger.Logger(c).WithError(err).Errorf("cannot create child client, id: [%s]", clientID)
			return nil, err
		}
	}
	// 有任何失败，返回
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot get client, id: [%s]", clientID)
		return nil, err
	}

	return clientEntity, nil
}
