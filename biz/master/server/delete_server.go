package server

import (
	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
)

func DeleteServerHandler(c *app.Context, req *pb.DeleteServerRequest) (*pb.DeleteServerResponse, error) {
	var (
		userServerID = req.GetServerId()
		userInfo     = common.GetUserInfo(c)
	)

	if !userInfo.Valid() {
		return &pb.DeleteServerResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid user"},
		}, nil
	}

	if len(userServerID) == 0 {
		return &pb.DeleteServerResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid client id"},
		}, nil
	}

	if err := dao.CascadeDeleteServer(c, userInfo, userServerID); err != nil {
		return nil, err
	}

	return &pb.DeleteServerResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
