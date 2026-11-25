package workerd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"text/template"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/samber/lo"
)

func BuildCapfile(workers []*pb.Worker) map[string]string {
	log := logger.Logger(context.Background()).WithField("op", "BuildCapfile")
	if len(workers) == 0 {
		return map[string]string{}
	}

	results := map[string]string{}
	for _, worker := range workers {
		tmpWorker := &pb.Worker{
			WorkerId:  lo.ToPtr(SafeWorkerID(worker.GetWorkerId())),
			UserId:    lo.ToPtr(worker.GetUserId()),
			CodeEntry: lo.ToPtr(worker.GetCodeEntry()),
			Socket: &pb.Socket{
				Name:    lo.ToPtr(worker.GetWorkerId()),
				Address: lo.ToPtr(worker.GetSocket().GetAddress()),
			},
			ConfigTemplate: lo.ToPtr(worker.GetConfigTemplate()),
		}

		writer := new(bytes.Buffer)
		capTemplate := template.New("capfile")
		workerTemplate := tmpWorker.GetConfigTemplate()
		if workerTemplate == "" {
			workerTemplate = defs.DefaultConfigTemplate
		}

		capTemplate, err := capTemplate.Parse(workerTemplate)
		if err != nil {
			// 模板解析失败不 panic：capnp 配置是非 HTML 文本，应用 text/template；
			// 用户可控模板此前解析失败会每 30s 刷一次 panic 日志
			log.Errorf("parse worker config template failed, worker id: [%s], err: %v", worker.GetWorkerId(), err)
			continue
		}
		capTemplate.Execute(writer, tmpWorker)

		results[worker.GetWorkerId()] = writer.String()
	}
	return results
}

func GenWorkerConfig(worker *pb.Worker, dir string) error {
	if worker == nil || worker.GetWorkerId() == "" {
		return errors.New("error worker")
	}
	fileMap := BuildCapfile([]*pb.Worker{worker})

	fileContent, ok := fileMap[worker.GetWorkerId()]
	if !ok {
		return errors.New("BuildCapfile error")
	}

	return utils.WriteFile(
		filepath.Join(
			dir, defs.WorkerInfoPath,
			worker.GetWorkerId(), defs.CapFileName,
		), fileContent)
}
