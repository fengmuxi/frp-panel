package server

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func InitServerHandler(c *app.Context, req *pb.InitServerRequest) (*pb.InitServerResponse, error) {
	var (
		userServerID = req.GetServerId()
		serverIP     = req.GetServerIp()
		comment      = strings.TrimSpace(req.GetComment())
		userInfo     = common.GetUserInfo(c)
	)

	if !userInfo.Valid() {
		return &pb.InitServerResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	if len(serverIP) == 0 {
		return &pb.InitServerResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "request invalid"},
		}, nil
	}

	// 兼容旧调用：请求中带了合法自定义 ID 则沿用，否则后端自动生成唯一 ID
	if len(userServerID) > 0 {
		if !utils.IsClientIDPermited(userServerID) {
			return &pb.InitServerResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "request invalid"},
			}, nil
		}
	} else {
		generated, genErr := generateUniqueServerID(c, userInfo.GetUserName())
		if genErr != nil {
			logger.Logger(c).WithError(genErr).Errorf("generate unique server id failed")
			return nil, genErr
		}
		userServerID = generated
	}

	// 面板界面要求必填名称；旧调用不传名称时以节点 ID 兜底
	if len(comment) == 0 {
		comment = userServerID
	}

	// 名称不允许重复
	dupCount, err := dao.NewQuery(c).CountServersByComment(userInfo, comment, "")
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("count servers by comment failed, comment: [%s]", comment)
		return nil, err
	}
	if dupCount > 0 {
		return &pb.InitServerResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "server name already exists"},
		}, nil
	}

	globalServerID := app.GlobalClientID(userInfo.GetUserName(), "s", userServerID)

	if err := dao.NewMutation(c).CreateServer(userInfo,
		&models.ServerEntity{
			ServerID:      globalServerID,
			TenantID:      userInfo.GetTenantID(),
			UserID:        userInfo.GetUserID(),
			ConnectSecret: uuid.New().String(),
			ServerIP:      serverIP,
			Comment:       comment,
		}); err != nil {
		return nil, err
	}

	return &pb.InitServerResponse{
		Status:   &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		ServerId: &globalServerID,
	}, nil
}

// generateUniqueServerID 生成不与现有服务端冲突的随机 ID（8 位字母数字），最多重试 10 次。
func generateUniqueServerID(c *app.Context, username string) (string, error) {
	q := dao.NewQuery(c)
	for i := 0; i < 10; i++ {
		suffix := utils.RandomString(8)
		globalID := app.GlobalClientID(username, "s", suffix)
		_, err := q.AdminGetServerByServerID(globalID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return suffix, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("failed to generate unique server id")
}
