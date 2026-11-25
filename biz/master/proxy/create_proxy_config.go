package proxy

import (
	"errors"
	"fmt"

	"github.com/fengmuxi/frp-panel/biz/master/client"
	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/fengmuxi/frp-panel/utils/logger"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/samber/lo"

	"gorm.io/gorm"
)

func CreateProxyConfig(c *app.Context, req *pb.CreateProxyConfigRequest) (*pb.CreateProxyConfigResponse, error) {

	if len(req.GetClientId()) == 0 || len(req.GetServerId()) == 0 || len(req.GetConfig()) == 0 {
		return nil, fmt.Errorf("request invalid")
	}

	var (
		userInfo = common.GetUserInfo(c)
		clientID = req.GetClientId()
		serverID = req.GetServerId()
	)

	clientEntity, err := GetClientWithMakeShadow(c, clientID, serverID)
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot get client, id: [%s]", clientID)
		return nil, err
	}

	_, err = dao.NewQuery(c).GetServerByServerID(userInfo, serverID)
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot get server, id: [%s]", serverID)
		return nil, err
	}

	typedProxyCfgs, err := utils.LoadProxiesFromContent(req.GetConfig())
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot load proxies from content")
		return nil, err
	}
	if len(typedProxyCfgs) == 0 || len(typedProxyCfgs) > 1 {
		logger.Logger(c).Errorf("invalid config, cfg len: [%d]", len(typedProxyCfgs))
		return nil, fmt.Errorf("invalid config")
	}

	if err := CreateProxyConfigWithTypedConfig(c, CreateProxyConfigWithTypedConfigParam{
		ClientID:     clientID,
		ServerID:     serverID,
		ProxyCfg:     typedProxyCfgs[0],
		ClientEntity: clientEntity,
		Overwrite:    req.GetOverwrite(),
	}); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot create proxy config")
		return nil, err
	}

	return &pb.CreateProxyConfigResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}

type CreateProxyConfigWithTypedConfigParam struct {
	ClientID     string
	ServerID     string
	ProxyCfg     v1.TypedProxyConfig
	ClientEntity *models.ClientEntity
	Overwrite    bool
	WorkerID     *string
}

func CreateProxyConfigWithTypedConfig(c *app.Context, param CreateProxyConfigWithTypedConfigParam) error {
	var (
		userInfo      = common.GetUserInfo(c)
		clientID      = param.ClientID
		serverID      = param.ServerID
		clientEntity  = param.ClientEntity
		typedProxyCfg = param.ProxyCfg
		err           error
		overwrite     = param.Overwrite
	)

	proxyCfg := &models.ProxyConfigEntity{}

	if err := proxyCfg.FillClientConfig(clientEntity); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot fill client config, id: [%s]", clientID)
		return err
	}

	if err := proxyCfg.FillTypedProxyConfig(typedProxyCfg); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot fill typed proxy config")
		return err
	}

	var existedProxyCfg *models.ProxyConfig
	existedProxyCfg, err = dao.NewQuery(c).GetProxyConfigByOriginClientIDAndName(userInfo, clientID, proxyCfg.Name)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.Logger(c).WithError(err).Errorf("cannot get proxy config, id: [%s]", clientID)
		return err
	}

	if !overwrite && err == nil {
		logger.Logger(c).Errorf("proxy config already exist, cfg: [%+v]", proxyCfg)
		return fmt.Errorf("proxy config already exist")
	}

	// update client config
	if oldCfg, err := clientEntity.GetConfigContent(); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot get client config, id: [%s]", clientID)
		return err
	} else {
		oldCfg.Proxies = lo.Filter(oldCfg.Proxies, func(proxy v1.TypedProxyConfig, _ int) bool {
			return proxy.GetBaseConfig().Name != typedProxyCfg.GetBaseConfig().Name
		})
		oldCfg.Proxies = append(oldCfg.Proxies, typedProxyCfg)

		if err := clientEntity.SetConfigContent(*oldCfg); err != nil {
			logger.Logger(c).WithError(err).Errorf("cannot set client config, id: [%s]", clientID)
			return err
		}
	}

	rawCfg, err := clientEntity.MarshalJSONConfig()
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot marshal client config, id: [%s]", clientID)
		return err
	}

	_, err = client.UpdateFrpcHander(c, &pb.UpdateFRPCRequest{
		ClientId: &clientEntity.ClientID,
		ServerId: &serverID,
		Config:   rawCfg,
		Comment:  &clientEntity.Comment,
		FrpsUrl:  &clientEntity.FrpsUrl,
	})
	if err != nil {
		// 错误不能吞掉：UpdateFrpcHander 失败意味着 client 配置未持久化、
		// proxy_config 行未创建（本函数自身不写 proxy_config，全靠该步骤的 rebuild）。
		// 若此时继续走下方跨服务器清理分支，会删掉旧 server 的隧道而新隧道未建成——数据丢失。
		// 前端此前会误显示创建成功，现在正确返回失败
		logger.Logger(c).WithError(err).Errorf("cannot update frpc, id: [%s]", clientID)
		return err
	}

	if existedProxyCfg != nil && existedProxyCfg.ServerID != serverID {
		logger.Logger(c).Warnf("client and server not match, delete old proxy, client: [%s], server: [%s], proxy: [%s]", clientID, serverID, proxyCfg.Name)
		if _, err := DeleteProxyConfig(c, &pb.DeleteProxyConfigRequest{
			ClientId: lo.ToPtr(existedProxyCfg.ClientID),
			ServerId: lo.ToPtr(existedProxyCfg.ServerID),
			Name:     &proxyCfg.Name,
		}); err != nil {
			logger.Logger(c).WithError(err).Errorf("cannot delete old proxy, client: [%s], server: [%s], proxy: [%s]", clientID, clientEntity.ServerID, proxyCfg.Name)
			return err
		}
	}

	return nil
}
