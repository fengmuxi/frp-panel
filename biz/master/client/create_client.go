package client

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

func InitClientHandler(c *app.Context, req *pb.InitClientRequest) (*pb.InitClientResponse, error) {
	userClientID := req.GetClientId()
	comment := strings.TrimSpace(req.GetComment())
	userInfo := common.GetUserInfo(c)

	if !userInfo.Valid() {
		return &pb.InitClientResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	// 兼容旧版 frpp：请求中带了合法自定义 ID 则沿用，否则后端自动生成唯一 ID
	if len(userClientID) > 0 {
		if !utils.IsClientIDPermited(userClientID) {
			return &pb.InitClientResponse{
				Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid client id"},
			}, nil
		}
	} else {
		generated, genErr := generateUniqueClientID(c, userInfo.GetUserName())
		if genErr != nil {
			logger.Logger(c).WithError(genErr).Errorf("generate unique client id failed")
			return nil, genErr
		}
		userClientID = generated
	}

	// 面板界面要求必填名称；命令行 join / 旧版二进制不传名称时以节点 ID 兜底
	if len(comment) == 0 {
		comment = userClientID
	}

	// 名称不允许重复（排除自身，用于改名场景的复用）
	dupCount, err := dao.NewQuery(c).CountClientsByComment(userInfo, comment, "")
	if err != nil {
		logger.Logger(c).WithError(err).Errorf("count clients by comment failed, comment: [%s]", comment)
		return nil, err
	}
	if dupCount > 0 {
		return &pb.InitClientResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "client name already exists"},
		}, nil
	}

	globalClientID := app.GlobalClientID(userInfo.GetUserName(), "c", userClientID)

	logger.Logger(c).Infof("start to init client, request:[%s], transformed global client id:[%s]", req.String(), globalClientID)

	if err := dao.NewMutation(c).CreateClient(userInfo,
		&models.ClientEntity{
			ClientID:      globalClientID,
			TenantID:      userInfo.GetTenantID(),
			UserID:        userInfo.GetUserID(),
			ConnectSecret: uuid.New().String(),
			Comment:       comment,
			IsShadow:      true,
			Ephemeral:     req.GetEphemeral(),
		}); err != nil {
		return &pb.InitClientResponse{Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()}}, err
	}

	return &pb.InitClientResponse{
		Status:   &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		ClientId: &globalClientID,
	}, nil
}

// generateUniqueClientID 生成不与现有客户端冲突的随机 ID（8 位字母数字），最多重试 10 次。
func generateUniqueClientID(c *app.Context, username string) (string, error) {
	q := dao.NewQuery(c)
	for i := 0; i < 10; i++ {
		suffix := utils.RandomString(8)
		globalID := app.GlobalClientID(username, "c", suffix)
		_, err := q.AdminGetClientByClientID(globalID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return suffix, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("failed to generate unique client id")
}
