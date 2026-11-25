package defs

import (
	"sync"

	"github.com/fengmuxi/frp-panel/pb"
)

// Connector 是一条节点/客户端 gRPC 流的连接句柄。
// Pending 是该连接上在途请求的响应表（sessionId → chan，连接私有）：
// 连接结束（流 EOF/错误）时 FailAllPending 一次性失败全部在途请求，
// 不再依赖 30s 超时兜底；此前是全局表，死连接的 pending 无法及时清理。
// 每次节点注册都会创建新的 Connector 实例，pending 天然按连接隔离，
// 重连后旧连接的表随之废弃（旧 goroutine 持有旧 connector 引用）。
type Connector struct {
	CliID   string
	Conn    pb.Master_ServerSendServer
	CliType string

	pendingMu sync.Mutex
	pending   map[string]chan *pb.ClientMessage
}

func (c *Connector) initPending() {
	if c.pending == nil {
		c.pending = map[string]chan *pb.ClientMessage{}
	}
}

// RegisterPending 登记一个在途请求并返回其响应 channel（带缓冲，
// 迟到投递不会阻塞也不会 panic）
func (c *Connector) RegisterPending(sessionID string) chan *pb.ClientMessage {
	c.initPending()
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	ch := make(chan *pb.ClientMessage, 1)
	c.pending[sessionID] = ch
	return ch
}

// DeletePending 移除一个在途请求（超时/完成后的清理）
func (c *Connector) DeletePending(sessionID string) {
	c.initPending()
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, sessionID)
}

// DeliverPending 投递响应；无在途等待（已超时删除）返回 false
func (c *Connector) DeliverPending(sessionID string, msg *pb.ClientMessage) bool {
	c.initPending()
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	ch, ok := c.pending[sessionID]
	if !ok {
		return false
	}
	delete(c.pending, sessionID)
	ch <- msg
	return true
}

// FailAllPending 连接结束时使全部在途请求立即失败返回（fail-fast），
// 并清空本连接的 pending 表
func (c *Connector) FailAllPending(reason string) {
	c.initPending()
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for sid, ch := range c.pending {
		ch <- &pb.ClientMessage{
			Event:     pb.Event_EVENT_ERROR,
			Data:      []byte(reason),
			SessionId: sid,
		}
	}
	c.pending = map[string]chan *pb.ClientMessage{}
}
