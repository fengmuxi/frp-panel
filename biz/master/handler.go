package master

import (
	"embed"
	"time"

	"github.com/VaalaCat/frp-panel/biz/master/auth"
	"github.com/VaalaCat/frp-panel/biz/master/client"
	"github.com/VaalaCat/frp-panel/biz/master/platform"
	"github.com/VaalaCat/frp-panel/biz/master/proxy"
	"github.com/VaalaCat/frp-panel/biz/master/server"
	"github.com/VaalaCat/frp-panel/biz/master/shell"
	"github.com/VaalaCat/frp-panel/biz/master/streamlog"
	"github.com/VaalaCat/frp-panel/biz/master/user"
	"github.com/VaalaCat/frp-panel/biz/master/worker"
	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/middleware"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/gin-gonic/gin"

	wgHandler "github.com/VaalaCat/frp-panel/biz/master/wg"
)

func NewRouter(fs embed.FS, appInstance app.Application, originCfg conf.Config) *gin.Engine {
	router := gin.New()
	// gin.Default = Logger + Recovery；此处用自定义 formatter 替换 Logger：
	// WS 场景 JWT 经 ?token= query 传递，默认日志会把带 token 的 URI 全文
	// 写入访问日志（日志持有者可窃取会话）
	router.Use(gin.LoggerWithFormatter(middleware.GinAccessLogFormatter()), gin.Recovery())
	// 默认信任所有代理会让 X-Forwarded-For 伪造 ClientIP——登录限流可被绕过；
	// 部署在可信反代后时请改为显式 CIDR 白名单
	_ = router.SetTrustedProxies(nil)
	HandleStaticFile(fs, router)
	ConfigureRouter(appInstance, originCfg, router)
	return router
}

func ConfigureRouter(appInstance app.Application, originCfg conf.Config, router *gin.Engine) {
	// 限流（安全审计 P1-5）：登录/注册/证书/认证插件端点此前零限流，可无限爆破
	authLimiter := middleware.NewRateLimiter(15*time.Minute, 20, 15*time.Minute)

	router.POST("/auth", authLimiter.Middleware(), auth.MakeGinHandlerFunc(appInstance, auth.HandleLogin))

	api := router.Group("/api")
	api.POST("/v1/auth/cert", authLimiter.Middleware(), app.Wrapper(appInstance, auth.GetClientCert))
	api.POST("/v1/auth/login", authLimiter.Middleware(), app.Wrapper(appInstance, auth.LoginHandler))
	api.POST("/v1/auth/register", authLimiter.Middleware(), app.Wrapper(appInstance, auth.RegisterHandler))
	// 登出用 POST：GET + cookie 自动携带可被跨站 <a> 触发强制登出（CSRF DoS）
	api.POST("/v1/auth/logout", auth.RemoveJWTHandler(appInstance))

	v1 := api.Group("/v1", middleware.JWTAuth(appInstance), middleware.AuthCtx(appInstance), middleware.RBAC(appInstance))
	{
		userRouter := v1.Group("/user")
		{
			userRouter.POST("/get", app.Wrapper(appInstance, user.GetUserInfoHandler))
			userRouter.POST("/update", app.Wrapper(appInstance, user.UpdateUserInfoHander))
			userRouter.POST("/sign-token", app.Wrapper(appInstance, user.SignTokenHandler))
		}
		platformRouter := v1.Group("/platform")
		{
			platformRouter.GET("/baseinfo", platform.GetPlatformInfo(appInstance, originCfg))
			platformRouter.POST("/clientsstatus", app.Wrapper(appInstance, platform.GetClientsStatus))
			platformRouter.POST("/smtp/get", platform.GetSMTPSettings(appInstance))
			platformRouter.POST("/smtp/update", platform.UpdateSMTPSettings(appInstance))
			platformRouter.POST("/smtp/test", platform.SendTestEmail(appInstance))
		}
		notifyRouter := v1.Group("/notify")
		{
			// 通知开关：所有登录用户读写自己的配置（无需管理员）
			notifyRouter.POST("/prefs/get", platform.GetNotifyPrefs(appInstance))
			notifyRouter.POST("/prefs/update", platform.UpdateNotifyPrefs(appInstance))
			notifyRouter.POST("/prefs/test", platform.SendNotifyTest(appInstance))
			// 资源级通知覆盖：三段式（不提醒/全局配置/独立配置），限本人资源或 admin
			notifyRouter.POST("/resource/get", platform.GetResourceNotifyPrefsHandler(appInstance))
			notifyRouter.POST("/resource/update", platform.UpdateResourceNotifyPrefsHandler(appInstance))
		}
		clientRouter := v1.Group("/client")
		{
			clientRouter.POST("/get", app.Wrapper(appInstance, client.GetClientHandler))
			clientRouter.POST("/init", app.Wrapper(appInstance, client.InitClientHandler))
			clientRouter.POST("/delete", app.Wrapper(appInstance, client.DeleteClientHandler))
			clientRouter.POST("/list", app.Wrapper(appInstance, client.ListClientsHandler))
			clientRouter.POST("/update_info", app.Wrapper(appInstance, client.UpdateClientInfoHandler))
			clientRouter.POST("/swap_sort", app.Wrapper(appInstance, client.SwapClientSortHandler))
			clientRouter.POST("/install_workerd", app.Wrapper(appInstance, worker.InstallWorkerd))
			clientRouter.POST("/upgrade", app.Wrapper(appInstance, client.UpgradeFrppHandler))
		}
		serverRouter := v1.Group("/server")
		{
			serverRouter.POST("/get", app.Wrapper(appInstance, server.GetServerHandler))
			serverRouter.POST("/init", app.Wrapper(appInstance, server.InitServerHandler))
			serverRouter.POST("/delete", app.Wrapper(appInstance, server.DeleteServerHandler))
			serverRouter.POST("/list", app.Wrapper(appInstance, server.ListServersHandler))
			serverRouter.POST("/update_info", app.Wrapper(appInstance, server.UpdateServerInfoHandler))
			serverRouter.POST("/swap_sort", app.Wrapper(appInstance, server.SwapServerSortHandler))
		}
		frpcRouter := v1.Group("/frpc")
		{
			frpcRouter.POST("/update", app.Wrapper(appInstance, client.UpdateFrpcHander))
			frpcRouter.POST("/delete", app.Wrapper(appInstance, client.RemoveFrpcHandler))
			frpcRouter.POST("/stop", app.Wrapper(appInstance, client.StopFRPCHandler))
			frpcRouter.POST("/start", app.Wrapper(appInstance, client.StartFRPCHandler))
		}
		frpsRouter := v1.Group("/frps")
		{
			frpsRouter.POST("/update", app.Wrapper(appInstance, server.UpdateFrpsHander))
			frpsRouter.POST("/delete", app.Wrapper(appInstance, server.RemoveFrpsHandler))
		}
		proxyRouter := v1.Group("/proxy")
		{
			proxyRouter.POST("/traffic_stats", app.Wrapper(appInstance, proxy.GetProxyTrafficStats))
			proxyRouter.POST("/get_by_cid", app.Wrapper(appInstance, proxy.GetProxyStatsByClientID))
			proxyRouter.POST("/get_by_sid", app.Wrapper(appInstance, proxy.GetProxyStatsByServerID))
			proxyRouter.POST("/list_configs", app.Wrapper(appInstance, proxy.ListProxyConfigs))
			proxyRouter.POST("/create_config", app.Wrapper(appInstance, proxy.CreateProxyConfig))
			proxyRouter.POST("/update_config", app.Wrapper(appInstance, proxy.UpdateProxyConfig))
			proxyRouter.POST("/update_rules", app.Wrapper(appInstance, proxy.UpdateProxyRules))
			proxyRouter.POST("/delete_config", app.Wrapper(appInstance, proxy.DeleteProxyConfig))
			proxyRouter.POST("/get_config", app.Wrapper(appInstance, proxy.GetProxyConfig))
			proxyRouter.POST("/start_proxy", app.Wrapper(appInstance, proxy.StartProxy))
			proxyRouter.POST("/stop_proxy", app.Wrapper(appInstance, proxy.StopProxy))
		}
		workerHandler := v1.Group("/worker")
		{
			workerHandler.POST("/get", app.Wrapper(appInstance, worker.GetWorker))
			workerHandler.POST("/status", app.Wrapper(appInstance, worker.GetWorkerStatus))
			workerHandler.POST("/create", app.Wrapper(appInstance, worker.CreateWorker))
			workerHandler.POST("/list", app.Wrapper(appInstance, worker.ListWorkers))
			workerHandler.POST("/remove", app.Wrapper(appInstance, worker.RemoveWorker))
			workerHandler.POST("/update", app.Wrapper(appInstance, worker.UpdateWorker))
			workerHandler.POST("/redeploy", app.Wrapper(appInstance, worker.RedeployWorker))
			workerHandler.POST("/create_ingress", app.Wrapper(appInstance, worker.CreateWorkerIngress))
			workerHandler.POST("/get_ingress", app.Wrapper(appInstance, worker.GetWorkerIngress))
		}

		wgRouter := v1.Group("/wg")
		{
			// network
			wgRouter.POST("/network/create", app.Wrapper(appInstance, wgHandler.CreateNetwork))
			wgRouter.POST("/network/delete", app.Wrapper(appInstance, wgHandler.DeleteNetwork))
			wgRouter.POST("/network/update", app.Wrapper(appInstance, wgHandler.UpdateNetwork))
			wgRouter.POST("/network/get", app.Wrapper(appInstance, wgHandler.GetNetwork))
			wgRouter.POST("/network/list", app.Wrapper(appInstance, wgHandler.ListNetworks))
			wgRouter.POST("/network/topology", app.Wrapper(appInstance, wgHandler.GetNetworkTopology))

			// endpoint
			wgRouter.POST("/endpoint/create", app.Wrapper(appInstance, wgHandler.CreateEndpoint))
			wgRouter.POST("/endpoint/delete", app.Wrapper(appInstance, wgHandler.DeleteEndpoint))
			wgRouter.POST("/endpoint/update", app.Wrapper(appInstance, wgHandler.UpdateEndpoint))
			wgRouter.POST("/endpoint/get", app.Wrapper(appInstance, wgHandler.GetEndpoint))
			wgRouter.POST("/endpoint/list", app.Wrapper(appInstance, wgHandler.ListEndpoints))

			// link
			wgRouter.POST("/link/create", app.Wrapper(appInstance, wgHandler.CreateWireGuardLink))
			wgRouter.POST("/link/delete", app.Wrapper(appInstance, wgHandler.DeleteWireGuardLink))
			wgRouter.POST("/link/update", app.Wrapper(appInstance, wgHandler.UpdateWireGuardLink))
			wgRouter.POST("/link/get", app.Wrapper(appInstance, wgHandler.GetWireGuardLink))
			wgRouter.POST("/link/list", app.Wrapper(appInstance, wgHandler.ListWireGuardLinks))

			// wireguard
			wgRouter.POST("/create", app.Wrapper(appInstance, wgHandler.CreateWireGuard))
			wgRouter.POST("/delete", app.Wrapper(appInstance, wgHandler.DeleteWireGuard))
			wgRouter.POST("/update", app.Wrapper(appInstance, wgHandler.UpdateWireGuard))
			wgRouter.POST("/restart", app.Wrapper(appInstance, wgHandler.RestartWireGuard))
			wgRouter.POST("/get", app.Wrapper(appInstance, wgHandler.GetWireGuard))
			wgRouter.POST("/list", app.Wrapper(appInstance, wgHandler.ListWireGuards))
			wgRouter.POST("/runtime/get", app.Wrapper(appInstance, wgHandler.GetWireGuardRuntimeInfo))
		}

		v1.GET("/pty/:clientID", shell.PTYHandler(appInstance))
		v1.GET("/log", streamlog.GetLogHandler(appInstance))
	}
}
