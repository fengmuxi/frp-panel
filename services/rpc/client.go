package rpc

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/fengmuxi/frp-panel/common"
	"github.com/fengmuxi/frp-panel/pb"
	"github.com/fengmuxi/frp-panel/services/app"
	"github.com/fengmuxi/frp-panel/utils/logger"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// callClientTimeout 单次节点 RPC 的等待上限：
// 节点假死（连接半开）时旧实现会永久阻塞在响应 channel 上，
// 造成调用方 goroutine 与 ClientRecvMap 条目双泄漏（HTTP 请求悬挂、对账任务卡死）。
// 连接正常关闭时会 FailAllPending 立即唤醒全部等待者，超时只是兜底
const callClientTimeout = 30 * time.Second

func CallClientWrapper[R common.RespType](c *app.Context, clientID string, event pb.Event, req proto.Message, resp *R) error {
	cresp, err := CallClient(c, clientID, event, req)
	if err != nil {
		return err
	}

	protoMsgRef, ok := any(resp).(protoreflect.ProtoMessage)
	if !ok {
		return fmt.Errorf("type does not implement protoreflect.ProtoMessage")
	}

	return proto.Unmarshal(cresp.GetData(), protoMsgRef)
}

// CallClient 向节点发送一条 RPC 并等待响应（带超时）。
// 在途请求登记在该连接私有的 pending 表上（defs.Connector），
// 连接结束（流 EOF/错误/被移除）时全部在途请求立即失败返回。
func CallClient(ctx *app.Context, clientID string, event pb.Event, msg proto.Message) (*pb.ClientMessage, error) {
	sender := ctx.GetApp().GetClientsManager().Get(clientID)
	if sender == nil {
		logger.Logger(ctx).Errorf("cannot get client, id: [%s]", clientID)
		return nil, fmt.Errorf("cannot get client, id: [%s]", clientID)
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("cannot marshal")
		return nil, err
	}

	req := &pb.ServerMessage{
		Event:     event,
		Data:      data,
		SessionId: uuid.New().String(),
		ClientId:  clientID,
	}

	respCh := sender.RegisterPending(req.SessionId)
	defer sender.DeletePending(req.SessionId)

	err = sender.Conn.Send(req)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Errorf("cannot send")
		ctx.GetApp().GetClientsManager().Remove(clientID)
		return nil, err
	}

	select {
	case resp := <-respCh:
		if resp.Event == pb.Event_EVENT_ERROR {
			return nil, fmt.Errorf("client return error: %s", resp.Data)
		}
		return resp, nil
	case <-time.After(callClientTimeout):
		return nil, fmt.Errorf("call client timeout after %s, event: [%s], client id: [%s]", callClientTimeout, event.String(), clientID)
	}
}

// Recv 启动节点 gRPC 流的接收循环，返回的 channel 在流结束（EOF/错误/连接被移除）时收到通知。
//
// 关键实现约束：
//   - connector 在循环外捕获一次：每轮重新 Get 会拿到重连后的新 connector，
//     导致新旧两个 Recv goroutine 并发 Recv 同一流（gRPC 禁止），响应被错误分发；
//   - connector 被移除（CallClient 发送失败会 Remove）时退出循环并发 done：
//     此前是无 sleep 的 continue 忙转（CPU 空转），且流的 EOF 永远观察不到，
//     done 不发出 → ServerSend 永久阻塞、CheckNodeOffline 永不启动（掉线漏报）；
//   - 流结束时 FailAllPending 立即失败该连接全部在途请求（等待方收到明确错误）。
func Recv(appInstance app.Application, clientID string) chan bool {
	done := make(chan bool)
	go func() {
		c := context.Background()
		log := logger.Logger(c).WithField("clientID", clientID)

		reciver := appInstance.GetClientsManager().Get(clientID)
		if reciver == nil {
			log.Errorf("cannot get client, exit recv loop")
			done <- true
			return
		}

		for {
			resp, err := reciver.Conn.Recv()
			if err == io.EOF {
				log.Infof("finish client recv")
				reciver.FailAllPending("client stream closed")
				done <- true
				return
			}
			if err != nil {
				log.WithError(err).Errorf("cannot recv, usually means client disconnect")
				reciver.FailAllPending("client stream closed")
				done <- true
				return
			}

			if !reciver.DeliverPending(resp.SessionId, resp) {
				log.Debugf("recv response has no pending caller (timeout or client switch), session: [%s]", resp.SessionId)
				continue
			}
			log.Debugf("recv success, resp: %+v", resp)
		}
	}()
	return done
}
