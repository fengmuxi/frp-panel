package clientrpc

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/pb"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/google/uuid"
)

// func clientHandleServerSend(req *pb.ServerMessage) *pb.ClientMessage {
// 	logger.Logger(c).Infof("client get a server message, origin is: [%+v]", req)
// 	return &pb.ClientMessage{
// 		Event:     pb.Event_EVENT_DATA,
// 		ClientId:  req.ClientId,
// 		SessionId: req.SessionId,
// 		Data:      req.Data,
// 	}
// }

// registClientToMaster 在当前 stream 上向 Master 发起注册。
// gRPC stream 一旦出错便不可复用，因此任何错误都直接返回，
// 由外层 startClientRpcHandler 重建 stream 后重试，避免卡死或 Fatal 退出。
func registClientToMaster(appInstance app.Application, recvStream pb.Master_ServerSendClient, event pb.Event, clientID, clientSecret string) error {
	ctx := context.Background()
	logger.Logger(ctx).Infof("start to regist client to master")
	for {
		err := recvStream.Send(&pb.ClientMessage{
			Event:     event,
			ClientId:  clientID,
			SessionId: uuid.New().String(),
			Secret:    clientSecret,
		})
		if err != nil {
			return fmt.Errorf("cannot send register message: %w", err)
		}

		resp, err := recvStream.Recv()
		if err != nil {
			return fmt.Errorf("cannot receive register response: %w", err)
		}

		// master 拒绝注册时回包 Data 携带错误说明（event 与请求相同，不能只看 event 判成功）
		if msg := string(resp.GetData()); strings.Contains(msg, "invalid") {
			return fmt.Errorf("master refused register, id: [%s], reason: %s", clientID, msg)
		}

		if resp.GetEvent() == event {
			logger.Logger(ctx).Infof("client get server register envent success, clientID: %s", resp.GetClientId())
			return nil
		}
	}
}

func runCLientRpcHandler(appInstance app.Application, recvStream pb.Master_ServerSendClient, done chan bool, clientID string,
	clientHandleServerSend func(appInstance app.Application, req *pb.ServerMessage) *pb.ClientMessage) {
	c := context.Background()
	for {
		select {
		case <-done:
			logger.Logger(c).Infof("finish rpc client")
			recvStream.CloseSend()
			return
		default:
			resp, err := recvStream.Recv()
			if err == io.EOF {
				// 注意：break 只会跳出 select，必须 return 才能让外层循环重建 stream
				logger.Logger(c).Infof("server closed stream, return to recreate")
				return
			}
			if err != nil {
				logger.Logger(context.Background()).WithError(err).Errorf("cannot receive, sleep 3s and return")
				time.Sleep(3 * time.Second)
				return
			}
			if resp == nil {
				continue
			}
			go func() {
				defer func() {
					if err := recover(); err != nil {
						logger.Logger(c).Errorf("catch panic, err: %v", err)
					}
				}()
				msg := clientHandleServerSend(appInstance, resp)
				if msg == nil {
					return
				}
				msg.ClientId = clientID
				msg.SessionId = resp.SessionId
				recvStream.Send(msg)
				logger.Logger(c).Infof("client resp received: %s", resp.GetClientId())
			}()
		}
	}
}

func startClientRpcHandler(appInstance app.Application, client app.MasterClient, done chan bool, clientID, clientSecret string, event pb.Event,
	clientHandleServerSend func(appInstance app.Application, req *pb.ServerMessage) *pb.ClientMessage) {
	c := context.Background()
	logger.Logger(c).Infof("start to run rpc client")
	for {
		select {
		case <-done:
			logger.Logger(c).Infof("finish rpc client")
			return
		default:
			recvStream, err := client.Call().ServerSend(context.Background())
			if err != nil {
				logger.Logger(context.Background()).WithError(err).Errorf("cannot create stream, sleep 3s and retry")
				time.Sleep(3 * time.Second)
				continue
			}

			if err := registClientToMaster(appInstance, recvStream, event, clientID, clientSecret); err != nil {
				logger.Logger(context.Background()).WithError(err).Errorf("cannot register to master, sleep 3s and recreate stream")
				recvStream.CloseSend()
				time.Sleep(3 * time.Second)
				continue
			}

			runCLientRpcHandler(appInstance, recvStream, done, clientID, clientHandleServerSend)
		}
	}
}
