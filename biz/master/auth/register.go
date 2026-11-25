package auth

import (
	"fmt"
	"sync"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/utils"
	"github.com/google/uuid"
)

// registerMu 串行化注册流程（安全审计 P2）：首个注册者会被提升为 admin，
// "查用户数 + 建用户" 两步存在 TOCTOU 竞态 —— 并发注册可产生多个 admin。
// master 为单进程，进程级互斥即可消除该竞态（用户名另有唯一索引兜底）。
var registerMu sync.Mutex

func RegisterHandler(c *app.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	registerMu.Lock()
	defer registerMu.Unlock()

	username := req.GetUsername()
	password := req.GetPassword()
	email := req.GetEmail()

	if username == "" || password == "" || email == "" {
		return &pb.RegisterResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "invalid username or password or email"},
		}, fmt.Errorf("invalid username or password or email")
	}

	userCount, err := dao.NewQuery(c).AdminCountUsers()
	if err != nil {
		return &pb.RegisterResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	if !c.GetApp().GetConfig().App.EnableRegister && userCount > 0 {
		return &pb.RegisterResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: "register is disabled"},
		}, fmt.Errorf("register is disabled")
	}

	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		return &pb.RegisterResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	newUser := &models.UserEntity{
		UserName: username,
		Password: hashedPassword,
		Email:    email,
		Status:   models.STATUS_NORMAL,
		Role:     defs.UserRole_Normal,
		Token:    uuid.New().String(),
	}

	if userCount == 0 {
		newUser.Role = defs.UserRole_Admin
	}

	err = dao.NewMutation(c).CreateUser(newUser)
	if err != nil {
		return &pb.RegisterResponse{
			Status: &pb.Status{Code: pb.RespCode_RESP_CODE_INVALID, Message: err.Error()},
		}, err
	}

	return &pb.RegisterResponse{
		Status: &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
	}, nil
}
