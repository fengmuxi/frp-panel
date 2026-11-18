package platform

import (
	"fmt"

	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/conf"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/gin-gonic/gin"
)

func GetPlatformInfo(appInstance app.Application, originCfg conf.Config) func(*gin.Context) {
	return func(c *gin.Context) {
		// 安全审计 P3：平台信息含 master RPC 地址/端口等内部拓扑，仅管理员可见
		if u := common.GetUserInfo(c); u == nil || !u.IsAdmin() {
			common.ErrResp(c, &pb.CommonResponse{Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "permission denied"}}, "permission denied")
			return
		}
		resp, err := getPlatformInfo(appInstance, originCfg, c)
		if err != nil {
			common.ErrResp(c, &pb.CommonResponse{Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()}}, err.Error())
			return
		}
		common.OKResp(c, resp)
	}
}

func getPlatformInfo(appInstance app.Application, originCfg conf.Config, c *gin.Context) (*pb.GetPlatformInfoResponse, error) {
	appCtx := app.NewContext(c, appInstance)
	userInfo := common.GetUserInfo(c)
	if !userInfo.Valid() {
		return &pb.GetPlatformInfoResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}
	totalServers, err := dao.NewQuery(appCtx).CountServers(userInfo)
	if err != nil {
		return nil, err
	}
	totalClients, err := dao.NewQuery(appCtx).CountClients(userInfo)
	if err != nil {
		return nil, err
	}

	configuredServers, err := dao.NewQuery(appCtx).CountConfiguredServers(userInfo)
	if err != nil {
		return nil, err
	}
	configuredClients, err := dao.NewQuery(appCtx).CountConfiguredClients(userInfo)
	if err != nil {
		return nil, err
	}

	unconfiguredServers := totalServers - configuredServers

	unconfiguredClients := totalClients - configuredClients

	// 对外宣告必须读 originConfig（env 原值）：appInstance 里的 Client.RPCUrl/APIUrl
	// 已被 NewDefaultServerConfig 改写为进程内回环拨号地址，不能下发给外部节点
	clientRPCUrl := originCfg.Client.RPCUrl
	clientAPIUrl := originCfg.Client.APIUrl

	if len(clientRPCUrl) == 0 {
		clientRPCUrl = fmt.Sprintf("grpc://%s:%d", originCfg.Master.RPCHost, originCfg.Master.RPCPort)
	}

	if len(clientAPIUrl) == 0 {
		clientAPIUrl = fmt.Sprintf("%s://%s:%d", originCfg.Master.APIScheme, originCfg.Master.RPCHost, originCfg.Master.APIPort)
	}

	return &pb.GetPlatformInfoResponse{
		Status:                  &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		TotalClientCount:        int32(totalClients),
		TotalServerCount:        int32(totalServers),
		UnconfiguredClientCount: int32(unconfiguredClients),
		UnconfiguredServerCount: int32(unconfiguredServers),
		ConfiguredClientCount:   int32(configuredClients),
		ConfiguredServerCount:   int32(configuredServers),
		MasterRpcHost:           appInstance.GetConfig().Master.RPCHost,
		MasterRpcPort:           int32(appInstance.GetConfig().Master.RPCPort),
		MasterApiPort:           int32(appInstance.GetConfig().Master.APIPort),
		MasterApiScheme:         appInstance.GetConfig().Master.APIScheme,
		ClientRpcUrl:            clientRPCUrl,
		ClientApiUrl:            clientAPIUrl,
		GithubProxyUrl:          appInstance.GetConfig().App.GithubProxyUrl,
	}, nil
}
