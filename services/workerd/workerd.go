package workerd

import (
	"context"
	"os"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/utils/logger"
)

var _ app.WorkerController = (*workerdController)(nil)

type workerdController struct {
	worker     *pb.Worker
	workerdCwd string
	status     *defs.WorkerStatus
}

func NewWorkerdController(worker *pb.Worker, workerdCwd string) *workerdController {
	return &workerdController{
		worker:     worker,
		workerdCwd: workerdCwd,
	}
}

func (w *workerdController) RunWorker(c *app.Context) {
	if err := w.Init(c); err != nil {
		logger.Logger(c).WithError(err).Errorf("init worker failed, workerId: [%s]", w.worker.GetWorkerId())
		return
	}

	execMgr := c.GetApp().GetWorkerExecManager()
	execMgr.RunCmd(
		w.worker.GetWorkerId(), WorkerCWDPath(c, w.worker, w.workerdCwd),
		[]string{ConfigFilePath(c, w.worker, w.workerdCwd)},
	)
}

func (w *workerdController) StopWorker(c *app.Context) {
	execMgr := c.GetApp().GetWorkerExecManager()
	execMgr.ExitCmd(w.worker.GetWorkerId())
	w.GarbageCollect()
}

func (w *workerdController) GetWorkerStatus(c *app.Context) defs.WorkerStatus {
	if w.status == nil {
		return defs.WorkerStatus_Unknown
	}
	return *w.status
}

func (w *workerdController) Init(c *app.Context) error {
	// 路径安全断言（安全审计 P0-2）：建目录/写文件前先确认路径未逃逸
	if err := EnsureWorkerPathsSafe(c, w.worker, w.workerdCwd); err != nil {
		logger.Logger(c).Errorf("worker path validation failed, workerId: [%s], err: %v", w.worker.GetWorkerId(), err)
		return err
	}

	workerCodePath := WorkerCodeRootPath(c, w.worker, w.workerdCwd)

	// 1. 创建工作目录（0700：目录内为租户代码与配置，安全审计 P3）
	if err := os.MkdirAll(workerCodePath, 0o700); err != nil {
		logger.Logger(c).WithError(err).Errorf("create work dir failed, path: [%s]", workerCodePath)
		return err
	}

	// 2. 写入配置文件和代码文件
	if err := WriteWorkerCodeToFile(c, w.worker, w.workerdCwd); err != nil {
		logger.Logger(c).WithError(err).Errorf("write worker code failed, workerId: [%s]", w.worker.GetWorkerId())
		return err
	}

	if err := GenCapnpConfig(c, w.workerdCwd, &pb.WorkerList{Workers: []*pb.Worker{w.worker}}); err != nil {
		logger.Logger(c).WithError(err).Errorf("gen worker capnp config failed, workerId: [%s]", w.worker.GetWorkerId())
		return err
	}

	logger.Logger(c).Infof("init worker success, workerId: [%s], code path: [%s]", w.worker.GetWorkerId(), workerCodePath)

	// worker 目录与产物交给降权用户（安全审计 P1-12 / 决策点 F；windows 为 no-op）
	chownTree(workerCodePath)

	return nil
}

func (w *workerdController) GarbageCollect() {
	ctx := context.Background()

	pathToRemove := WorkerCWDPath(ctx, w.worker, w.workerdCwd)

	// containment 断言（安全审计 P2：原实现按 "/tmp" 前缀判断，workdir 非 /tmp 时
	// 形同虚设且存在绕过空间），仅允许删除 workerdCwd 内部的目录
	if err := ensureUnderCwd(w.workerdCwd, pathToRemove); err != nil {
		logger.Logger(ctx).Errorf("refuse to remove path outside workerd cwd, path: [%s], err: %v", pathToRemove, err)
		return
	}

	if err := os.RemoveAll(pathToRemove); err != nil {
		logger.Logger(ctx).WithError(err).Errorf("remove path failed, path: [%s]", pathToRemove)
	}
}
