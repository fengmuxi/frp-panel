package proxy

import (
	"github.com/VaalaCat/frp-panel/common"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/services/dao"
	"github.com/VaalaCat/frp-panel/utils/logger"
)

// UpdateProxyRules 更新隧道自动规则（定时开关 + 流量限额）。
// 只修改面板元数据（proxy_config 表），不触碰 frp 配置、不向节点推送任何事件，
// 规则的实际生效由 Master 的周期对账任务在 1 分钟内完成。
// 语义为“整体替换”：请求携带完整规则集合（前端规则对话框提交全部字段）。
func UpdateProxyRules(c *app.Context, req *pb.UpdateProxyRulesRequest) (*pb.UpdateProxyRulesResponse, error) {
	logger.Logger(c).Infof("update proxy rules, req: [%+v]", req)

	var (
		userInfo  = common.GetUserInfo(c)
		clientID  = req.GetClientId()
		serverID  = req.GetServerId()
		proxyName = req.GetName()
	)

	if !userInfo.Valid() {
		return &pb.UpdateProxyRulesResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	if len(clientID) == 0 || len(serverID) == 0 || len(proxyName) == 0 {
		return &pb.UpdateProxyRulesResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid proxy identity"},
		}, nil
	}

	quotaType := req.GetQuotaType()
	if !QuotaTypeValid(quotaType) {
		return &pb.UpdateProxyRulesResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid quota type, expect day/week/month"},
		}, nil
	}
	if quotaType != "" && req.GetQuotaLimit() <= 0 {
		return &pb.UpdateProxyRulesResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "quota limit must be positive"},
		}, nil
	}

	var scheduleDays []int
	if req.GetScheduleEnabled() {
		if _, err := ParseHHMM(req.GetScheduleStart()); err != nil {
			return &pb.UpdateProxyRulesResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
			}, nil
		}
		if _, err := ParseHHMM(req.GetScheduleEnd()); err != nil {
			return &pb.UpdateProxyRulesResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
			}, nil
		}
		rawDays := req.GetScheduleDays()
		days := make([]int, 0, len(rawDays))
		for _, d := range rawDays {
			days = append(days, int(d))
		}
		scheduleDays = NormalizeScheduleDays(days)
	}

	proxyConfig, err := dao.NewQuery(c).GetProxyConfigByFilter(userInfo, &models.ProxyConfigEntity{
		ClientID: clientID,
		ServerID: serverID,
		Name:     proxyName,
	})
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot get proxy config, client: [%s], server: [%s], proxy name: [%s]", clientID, serverID, proxyName)
		return &pb.UpdateProxyRulesResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "cannot get proxy config"},
		}, nil
	}

	// 整体替换规则字段
	proxyConfig.ScheduleEnabled = req.GetScheduleEnabled()
	proxyConfig.ScheduleStart = req.GetScheduleStart()
	proxyConfig.ScheduleEnd = req.GetScheduleEnd()
	proxyConfig.ScheduleDays = scheduleDays
	proxyConfig.QuotaType = quotaType
	if quotaType == "" {
		proxyConfig.QuotaLimit = 0
	} else {
		proxyConfig.QuotaLimit = req.GetQuotaLimit()
	}

	if err := dao.NewMutation(c).UpdateProxyConfig(userInfo, proxyConfig); err != nil {
		logger.Logger(c).WithError(err).Errorf("cannot update proxy rules, client: [%s], server: [%s], proxy name: [%s]", clientID, serverID, proxyName)
		return nil, err
	}

	logger.Logger(c).Infof("update proxy rules success, client: [%s], server: [%s], proxy name: [%s], schedule: [%v], quota: [%s/%d]",
		clientID, serverID, proxyName, proxyConfig.ScheduleEnabled, proxyConfig.QuotaType, proxyConfig.QuotaLimit)

	return &pb.UpdateProxyRulesResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
