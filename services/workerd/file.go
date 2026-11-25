package workerd

import (
	"context"
	"path/filepath"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/utils"
)

func WriteWorkerCodeToFile(ctx context.Context, worker *pb.Worker, workerdCWD string) error {
	// 路径安全断言（安全审计 P0-2）：写入前确认路径合法且未逃逸出 workerd 目录
	if err := EnsureWorkerPathsSafe(ctx, worker, workerdCWD); err != nil {
		return err
	}
	return utils.WriteFile(
		CodeFilePath(ctx, worker, workerdCWD),
		string(worker.GetCode()))
}

func CodeFilePath(ctx context.Context, worker *pb.Worker, workerdCWD string) string {
	return filepath.Join(
		WorkerCWDPath(ctx, worker, workerdCWD),
		defs.WorkerCodePath,
		worker.GetCodeEntry())
}

func WorkerCodeRootPath(ctx context.Context, worker *pb.Worker, workerdCWD string) string {
	return filepath.Join(
		WorkerCWDPath(ctx, worker, workerdCWD),
		defs.WorkerCodePath)
}

func WorkerCWDPath(ctx context.Context, worker *pb.Worker, workerdCWD string) string {
	return filepath.Join(
		workerdCWD,
		defs.WorkerInfoPath,
		worker.GetWorkerId(),
	)
}

func ConfigFilePath(ctx context.Context, worker *pb.Worker, workerdCWD string) string {
	return filepath.Join(
		WorkerCWDPath(ctx, worker, workerdCWD),
		defs.CapFileName,
	)
}
