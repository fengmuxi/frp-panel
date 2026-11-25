package server

import (
	"context"
	"time"

	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/utils/logger"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/samber/lo"
)

var proxyTypeList = []v1.ProxyType{
	v1.ProxyTypeTCP,
	v1.ProxyTypeUDP,
	v1.ProxyTypeTCPMUX,
	v1.ProxyTypeHTTP,
	v1.ProxyTypeHTTPS,
	v1.ProxyTypeSTCP,
	v1.ProxyTypeXTCP,
	v1.ProxyTypeSUDP,
}

func PushProxyInfo(appInstance app.Application, serverID, serverSecret string) error {
	proxyInfos := []*pb.ProxyInfo{}

	if cli := appInstance.GetServerController().Get(serverID); cli != nil {
		firstSync := cli.IsFirstSync()
		// today_* 计数按 frps 所在机器的本地自然日切分（frp DateCounter），
		// 上报该归属日供 master 精确判断跨天翻转，避免节点与 master 时区
		// 不一致（或推送延迟跨零点）时误判。
		statsDate := lo.ToPtr(time.Now().Format("2006-01-02"))
		for _, proxyType := range proxyTypeList {
			proxyStatsList := cli.GetProxyStatsByType(proxyType)
			for _, proxyStats := range proxyStatsList {
				if proxyStats == nil {
					continue
				}
				proxyInfos = append(proxyInfos, &pb.ProxyInfo{
					Name:            lo.ToPtr(proxyStats.Name),
					Type:            lo.ToPtr(proxyStats.Type),
					TodayTrafficIn:  lo.ToPtr(proxyStats.TodayTrafficIn),
					TodayTrafficOut: lo.ToPtr(proxyStats.TodayTrafficOut),
					FirstSync:       lo.ToPtr(firstSync),
					StatsDate:       statsDate,
				})
			}
		}
	}

	if len(proxyInfos) > 0 {
		ctx := context.Background()
		cli := appInstance.GetMasterCli()
		_, err := cli.Call().PushProxyInfo(ctx, &pb.PushProxyInfoReq{
			Base: &pb.ServerBase{
				ServerId:     serverID,
				ServerSecret: serverSecret,
			},
			ProxyInfos: proxyInfos,
		})
		if err != nil {
			logger.Logger(context.Background()).WithError(err).Error("cannot push proxy info")
			return err
		}
		// 推送成功后才消费 firstSync：失败的推送下次重试仍带标记
		if handler := appInstance.GetServerController().Get(serverID); handler != nil {
			handler.MarkFirstSyncSent()
		}
	}
	return nil
}
