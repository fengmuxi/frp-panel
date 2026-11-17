package shared

import (
	"context"

	"github.com/VaalaCat/frp-panel/biz/master/auth"
	"github.com/VaalaCat/frp-panel/biz/master/proxy"
	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/cache"
	"github.com/VaalaCat/frp-panel/services/master"
	"github.com/VaalaCat/frp-panel/services/mux"
	"github.com/VaalaCat/frp-panel/services/watcher"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/VaalaCat/frp-panel/utils/wsgrpc"
	"github.com/gin-gonic/gin"
	"github.com/sourcegraph/conc"
	"go.uber.org/fx"
)

type runMasterParam struct {
	fx.In

	Lc fx.Lifecycle

	Ctx                 *app.Context
	AppInstance         app.Application
	DBManagerMgr        app.DBManager
	HTTPMuxServer       mux.MuxServer `name:"httpMux"`
	TLSMuxServer        mux.MuxServer `name:"tlsMux"`
	MasterRouter        *gin.Engine   `name:"masterRouter"`
	ClientLogManager    app.ClientLogManager
	WsGrpcHandler       gin.HandlerFunc      `name:"wsGrpcHandler"`
	MasterService       master.MasterService `name:"wsMasterService"`
	TaskManager         watcher.Client       `name:"masterTaskManager"`
	WsListener          *wsgrpc.WSListener
	DefaultServerConfig conf.Config `name:"defaultServerConfig"`
}

func runMaster(param runMasterParam) {
	// 安全基线（安全审计 P1-4）：默认/弱 GLOBAL_SECRET 会派生出可离线推导的 JWT 签名密钥，
	// 任何知道面板域名的人都能伪造管理员 token —— 必须拒绝启动
	cfg := param.AppInstance.GetConfig()
	if err := cfg.ValidateGlobalSecret(); err != nil {
		logger.Logger(context.Background()).Fatalf("security check failed: %v (set a strong APP_GLOBAL_SECRET and restart)", err)
	}

	param.AppInstance.SetClientLogManager(param.ClientLogManager)
	param.MasterRouter.GET("/wsgrpc", param.WsGrpcHandler)

	cache.InitCache(param.AppInstance.GetConfig())
	auth.InitAuth(param.AppInstance)

	param.TaskManager.AddCronTask("0 0 3 * * *", proxy.CollectDailyStats, param.AppInstance)
	// 每分钟对账一次隧道自动规则（定时开关 + 流量限额），边界时刻精确到分钟
	param.TaskManager.AddCronTask("0 * * * * *", proxy.ReconcileTunnelRules, param.AppInstance)
	// 每小时整点采集当日流量小时快照（首页"今日"小时粒度趋势的数据源）
	param.TaskManager.AddCronTask("0 0 * * * *", proxy.CollectHourlyStats, param.AppInstance)
	// master 启动时立即采集一次，保证部署后当天即可产生小时序列
	proxy.CollectHourlyStats(param.Ctx)
	defer param.TaskManager.Stop()

	logger.Logger(param.Ctx).Infof("start to run master")
	var wg conc.WaitGroup

	param.Lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			wg.Go(func() {
				if err := param.MasterService.GetServer().Serve(param.WsListener); err != nil {
					logger.Logger(param.Ctx).Fatalf("gRPC server error: %v", err)
				}
			})
			wg.Go(param.TLSMuxServer.Run)
			wg.Go(param.HTTPMuxServer.Run)
			wg.Go(param.TaskManager.Run)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			param.MasterService.GetServer().Stop()
			param.TLSMuxServer.Stop()
			param.HTTPMuxServer.Stop()
			param.TaskManager.Stop()
			wg.Wait()
			return nil
		},
	})
}
