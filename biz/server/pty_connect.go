package server

import (
	"github.com/fengmuxi/frp-panel/biz/common"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
)

func StartPTYConnect(c *app.Context, req *pb.CommonRequest) (*pb.CommonResponse, error) {
	return common.StartPTYConnect(c, req, &pb.PTYClientMessage{Base: &pb.PTYClientMessage_ServerBase{
		ServerBase: &pb.ServerBase{
			ServerId:     c.GetApp().GetConfig().Client.ID,
			ServerSecret: c.GetApp().GetConfig().Client.Secret,
		},
	}})
}
