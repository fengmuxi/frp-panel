package server

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/utils/logger"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/metrics/mem"
	"github.com/fatedier/frp/server"
	"github.com/sourcegraph/conc"
)

type serverImpl struct {
	srv           *server.Service
	Common        *v1.ServerConfig
	firstSyncSent atomic.Bool
}

func NewServerHandler(svrCfg *v1.ServerConfig) (app.ServerHandler, error) {
	svrCfg.Complete()
	ctx := context.Background()

	warning, err := validation.ValidateServerConfig(svrCfg)
	if warning != nil {
		logger.Logger(ctx).WithError(err).Warnf("validate server config warning: %+v", warning)
	}
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("invalid server config")
		return nil, fmt.Errorf("validate server config: %w", err)
	}

	var svr *server.Service

	if svr, err = server.NewService(svrCfg); err != nil {
		logger.Logger(ctx).WithError(err).Errorf("cannot create server service")
		return nil, fmt.Errorf("create server service: %w", err)
	}

	logger.Logger(ctx).Debugf("create server, config is: [ %+v ]", svrCfg)

	return &serverImpl{
		srv:    svr,
		Common: svrCfg,
	}, nil
}

func (s *serverImpl) Run() {
	wg := conc.NewWaitGroup()
	wg.Go(func() { s.srv.Run(context.Background()) })
	wg.Wait()
}

func (s *serverImpl) Stop() {
	c := context.Background()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		err := s.srv.Close()
		if err != nil {
			logger.Logger(c).Errorf("close server error: %v", err)
		}
		logger.Logger(c).Infof("server closed")
	})
	wg.Wait()
}

func (s *serverImpl) GetCommonCfg() *v1.ServerConfig {
	return s.Common
}

func (s *serverImpl) GetMem() *mem.ServerStats {
	return mem.StatsCollector.GetServer()
}

func (s *serverImpl) GetProxyStatsByType(proxyType v1.ProxyType) []*mem.ProxyStats {
	return mem.StatsCollector.GetProxiesByType(string(proxyType))
}

// IsFirstSync 返回该 frps 实例是否尚未成功完成过一次代理信息上报。
// 仅在推送成功送达 master 后由 MarkFirstSyncSent 消费：首次推送失败时
// 下次上报仍带 firstSync 标记，master 侧跨重启衔接补偿不会因此丢失。
func (s *serverImpl) IsFirstSync() bool {
	return !s.firstSyncSent.Load()
}

func (s *serverImpl) MarkFirstSyncSent() {
	s.firstSyncSent.Store(true)
}
