package client

import (
	"context"

	bizupgrade "github.com/VaalaCat/frp-panel/biz/common/upgrade"
	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

// UpgradeFrpp 收到 master 下发的升级指令后，异步执行升级并快速 ACK。
// 说明：必须快速返回 response，避免 master 端 HTTP/RPC 长时间阻塞；
//
//	真正的 stop/restart 将由 upgrader service/worker 执行（会导致连接短暂断开，属于预期）。
func UpgradeFrpp(ctx *app.Context, req *pb.UpgradeFrppRequest) (*pb.UpgradeFrppResponse, error) {
	log := logger.Logger(ctx)
	log.Infof("upgrade frpp request received, clientIds=%v version=%s downloadUrl=%s", req.GetClientIds(), req.GetVersion(), req.GetDownloadUrl())

	// 默认值处理（proto3 optional 未设置时 GetXXX 返回零值）
	backup := true
	if req.Backup != nil {
		backup = req.GetBackup()
	}
	restartService := true
	if req.RestartService != nil {
		restartService = req.GetRestartService()
	}

	// 安全审计 P1-11 + 决策点 B/G：节点侧执行参数不再信任请求值——
	//   TargetPath/WorkDir 用节点本地默认（当前二进制位置），
	//   ServiceName 用本机服务名常量（defs.DefaultServiceName），
	//   DownloadURL 保留但受官方源白名单 + SHA-256 清单校验约束，
	//   代理（GithubProxy/UseGithubProxy/HttpProxy）一律忽略请求值。
	// 原 Opts 曾把请求中的 TargetPath/ServiceName/ServiceArgs 原样下发，
	// 等于允许租户让节点以 root 覆盖任意路径并重启任意服务。
	opts := bizupgrade.Options{
		Version:        req.GetVersion(),
		DownloadURL:    req.GetDownloadUrl(),
		UseGithubProxy: false,
		Backup:         backup,
		ServiceName:    defs.DefaultServiceName,
		RestartService: restartService,
	}

	// 异步执行：确保能快速回 ACK，避免远程触发链路因重启/断连卡死
	go func() {
		bg := context.Background()
		if _, err := bizupgrade.StartWithResult(bg, opts); err != nil {
			logger.Logger(bg).WithError(err).Error("upgrade frpp failed")
		}
	}()

	return &pb.UpgradeFrppResponse{
		Status: &pb.Status{
			Code:    pb.RespCode_RESP_CODE_SUCCESS,
			Message: "accepted",
		},
	}, nil
}
