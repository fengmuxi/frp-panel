package wg

import (
	"errors"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/samber/lo"
)

func UpdateWireGuard(ctx *app.Context, req *pb.UpdateWireGuardRequest) (*pb.UpdateWireGuardResponse, error) {
	userInfo := common.GetUserInfo(ctx)
	if !userInfo.Valid() {
		return nil, errors.New("invalid user")
	}
	cfg := req.GetWireguardConfig()
	if cfg == nil || cfg.GetId() == 0 || len(cfg.GetClientId()) == 0 || len(cfg.GetInterfaceName()) == 0 || len(cfg.GetPrivateKey()) == 0 || len(cfg.GetLocalAddress()) == 0 {
		return nil, errors.New("invalid wireguard config")
	}
	q := dao.NewQuery(ctx)
	m := dao.NewMutation(ctx)

	model := &models.WireGuard{}
	model.FromPB(cfg)
	model.UserId = uint32(userInfo.GetUserID())
	model.TenantId = uint32(userInfo.GetTenantID())

	// LocalAddress 合法性校验（与 Create 同口径）：必须在网络 CIDR 内且未被占用；
	// 被占用时 AllocateIP 自动顺延——重复地址此前会让全网 30s pull 整体失败
	ips, err := q.GetWireGuardLocalAddressesByNetworkID(userInfo, uint(cfg.GetNetworkId()))
	if err != nil {
		return nil, err
	}
	network, err := q.GetNetworkByID(userInfo, uint(cfg.GetNetworkId()))
	if err != nil {
		return nil, err
	}
	newAddr, err := utils.AllocateIP(network.CIDR, ips, model.LocalAddress)
	if err != nil {
		return nil, err
	}
	model.LocalAddress = newAddr

	if err := m.UpdateWireGuard(userInfo, uint(cfg.GetId()), model); err != nil {
		return nil, err
	}

	// 端点赋值与解绑
	// 1) 读取当前绑定的端点
	currentList, err := q.ListEndpointsWithFilters(userInfo, 1, 1000, "", uint(cfg.GetId()), "")
	if err != nil {
		return nil, err
	}
	currentSet := lo.SliceToMap(currentList, func(e *models.Endpoint) (uint, struct{}) { return uint(e.ID), struct{}{} })

	// 2) 处理本次配置中的端点：存在则更新并绑定；不存在则创建绑定
	newSet := map[uint]struct{}{}
	for _, ep := range cfg.GetAdvertisedEndpoints() {
		if ep == nil {
			continue
		}
		if ep.GetId() > 0 {
			exist, err := q.GetEndpointByID(userInfo, uint(ep.GetId()))
			if err != nil {
				return nil, err
			}
			// 必须属于相同 client
			if exist.ClientID != cfg.GetClientId() {
				return nil, errors.New("endpoint client mismatch")
			}
			exist.WireGuardID = uint(cfg.GetId())

			if err := m.UpdateEndpoint(userInfo, uint(exist.ID), exist.EndpointEntity); err != nil {
				return nil, err
			}
			newSet[uint(exist.ID)] = struct{}{}
		} else {

			entity := &models.Endpoint{}
			entity.FromPB(ep)
			entity.WireGuardID = uint(cfg.GetId())
			entity.ClientID = cfg.GetClientId()

			if err := m.CreateEndpoint(userInfo, entity.EndpointEntity); err != nil {
				return nil, err
			}
		}
	}

	// 3) 解绑本次未包含的端点（将 WireGuardID 置 0）
	for id := range currentSet {
		if _, ok := newSet[id]; ok {
			continue
		}
		exist, err := q.GetEndpointByID(userInfo, id)
		if err != nil {
			return nil, err
		}
		entity := &models.Endpoint{}
		entity.FromPB(exist.ToPB())
		entity.WireGuardID = 0
		if err := m.UpdateEndpoint(userInfo, id, entity.EndpointEntity); err != nil {
			return nil, err
		}
	}
	return &pb.UpdateWireGuardResponse{Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "success"}, WireguardConfig: cfg}, nil
}
