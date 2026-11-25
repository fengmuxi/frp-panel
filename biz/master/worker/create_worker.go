package worker

import (
	"fmt"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/services/dao"
	"github.com/fengmuxi/frp-panel/services/rpc"
	"github.com/fengmuxi/frp-panel/services/workerd"
	"github.com/fengmuxi/frp-panel/utils/logger"
)

func CreateWorker(ctx *app.Context, req *pb.CreateWorkerRequest) (*pb.CreateWorkerResponse, error) {
	var (
		userInfo  = common.GetUserInfo(ctx)
		clientId  = req.GetClientId()
		reqWorker = req.GetWorker()
	)
	q := dao.NewQuery(ctx)
	m := dao.NewMutation(ctx)

	if err := validateCreateWorker(req); err != nil {
		logger.Logger(ctx).WithError(err).Errorf("invalid create worker request, origin is: [%s]", req.String())
		return nil, err
	}

	cli, err := q.GetClientByClientID(userInfo, clientId)
	if err != nil {
		logger.Logger(ctx).WithError(err).Errorf("cannot get client, id: [%s], workerName: [%s]", clientId, reqWorker.GetName())
		return nil, err
	}

	workerd.FillWorkerValue(reqWorker, uint(userInfo.GetUserID()))

	workerToCreate := (&models.Worker{}).FromPB(reqWorker)
	workerToCreate.WorkerModel = nil

	workerToCreate.Clients = append(workerToCreate.Clients, *cli)

	if err := m.CreateWorker(userInfo, workerToCreate); err != nil {
		logger.Logger(ctx).WithError(err).Errorf("cannot create worker, workerName: [%s]", workerToCreate.Name)
		return nil, err
	}

	go func() {
		bgCtx := ctx.Background()
		resp := &pb.CreateWorkerResponse{}
		err := rpc.CallClientWrapper(bgCtx, clientId, pb.Event_EVENT_CREATE_WORKER, req, resp)
		if err != nil {
			logger.Logger(bgCtx).WithError(err).Errorf("create worker event send to client error, client id: [%s], worker name: [%s]", clientId, workerToCreate.Name)
		}
	}()

	logger.Logger(ctx).Infof("create worker success, workerName: [%s], start to create worker's proxy", workerToCreate.Name)
	return &pb.CreateWorkerResponse{
		Status:   &pb.Status{Code: pb.RespCode_RESP_CODE_SUCCESS, Message: "ok"},
		WorkerId: &workerToCreate.ID,
	}, nil
}

func validateCreateWorker(req *pb.CreateWorkerRequest) error {
	if len(req.GetClientId()) == 0 {
		return fmt.Errorf("invalid client id")
	}

	if req.GetWorker() == nil {
		return fmt.Errorf("invalid worker")
	}

	// 路径安全（安全审计 P0-2）：WorkerId/CodeEntry 会用于构造节点上的文件路径，
	// 原样接受即可穿越写入任意文件；空值由 workerd.FillWorkerValue 服务端生成
	if req.GetWorker().GetWorkerId() != "" && !workerd.ValidateWorkerPathSegment(req.GetWorker().GetWorkerId()) {
		return fmt.Errorf("invalid worker id")
	}
	if req.GetWorker().GetCodeEntry() != "" && !workerd.ValidateWorkerPathSegment(req.GetWorker().GetCodeEntry()) {
		return fmt.Errorf("invalid worker code entry")
	}

	return nil
}
