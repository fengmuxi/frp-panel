package client

import (
	"fmt"

	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/utils/logger"
)

func InstallWorkerd(ctx *app.Context, req *pb.InstallWorkerdRequest) (*pb.InstallWorkerdResponse, error) {
	if !ctx.GetApp().GetConfig().Client.Features.EnableFunctions {
		logger.Logger(ctx).Errorf("function features are not enabled")
		return nil, fmt.Errorf("function features are not enabled")
	}

	workersMgr := ctx.GetApp().GetWorkersManager()

	// installDir 传空 → 安装到 /usr/local/bin（安全审计 P3：
	// 原实现装进 frpp 工作目录，cwd 下同名可执行文件会被劫持执行）

	binPath, err := workersMgr.InstallWorkerd(ctx, req.GetDownloadUrl(), "")
	if err != nil {
		logger.Logger(ctx).Errorf("failed to install workerd: %v", err)
		return nil, fmt.Errorf("failed to install workerd: %v", err)
	}

	execMgr := ctx.GetApp().GetWorkerExecManager()
	execMgr.UpdateBinaryPath(binPath)

	return &pb.InstallWorkerdResponse{
		Status: &pb.Status{
			Code:    pb.RespCode_RESP_CODE_SUCCESS,
			Message: "ok",
		},
	}, nil
}
