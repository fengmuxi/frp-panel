package client

import (
	"sync"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/services/rpc"
	"github.com/fengmuxi/frp-panel/utils/logger"
)

func UpgradeFrppHandler(ctx *app.Context, req *pb.UpgradeFrppRequest) (*pb.UpgradeFrppResponse, error) {
	userInfo := common.GetUserInfo(ctx)
	clientIds := req.GetClientIds()
	log := logger.Logger(ctx)
	log.Infof("upgrade frpp called, user=%v clientIds=%v", userInfo, clientIds)

	if len(clientIds) == 0 {
		return &pb.UpgradeFrppResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "client_ids is empty"},
		}, nil
	}

	// 默认值处理：proto3 optional 未设置时 GetXXX 返回零值
	backup := true
	if req.Backup != nil {
		backup = req.GetBackup()
	}
	restartService := true
	if req.RestartService != nil {
		restartService = req.GetRestartService()
	}
	// 代理与节点执行参数不再下发（安全审计 P1-11 / 决策点 G），节点使用本地真实值

	// 并发下发，提高多 client 批量升级速度
	var (
		wg      sync.WaitGroup
		errOnce error
		mu      sync.Mutex
	)

	for _, cid := range clientIds {
		clientId := cid
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, err := dao.NewQuery(ctx).GetClientByClientID(userInfo, clientId)
			if err != nil {
				mu.Lock()
				if errOnce == nil {
					errOnce = err
				}
				mu.Unlock()
				return
			}

			// 每次只给单个 client 下发（减少 payload 混淆）
			// 安全审计 P1-11：不再转发请求中的节点侧执行参数（DownloadUrl/GithubProxy/
			// TargetPath/ServiceName/Workdir/ServiceArgs 曾被节点原样执行），
			// 节点使用本地真实值；URL 由节点侧官方源白名单 + SHA-256 校验约束
			reqForClient := &pb.UpgradeFrppRequest{
				ClientIds:      []string{clientId},
				Version:        req.Version,
				Backup:         &backup,
				RestartService: &restartService,
			}

			resp := &pb.UpgradeFrppResponse{}
			if err := rpc.CallClientWrapper(ctx, clientId, pb.Event_EVENT_UPGRADE_FRPP, reqForClient, resp); err != nil {
				mu.Lock()
				if errOnce == nil {
					errOnce = err
				}
				mu.Unlock()
				return
			}
		}()
	}
	wg.Wait()

	if errOnce != nil {
		log.WithError(errOnce).Error("upgrade frpp dispatch failed")
		return nil, errOnce
	}

	return &pb.UpgradeFrppResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
