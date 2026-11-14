package dao

import "github.com/VaalaCat/frp-panel/services/app"

type Query interface {
	CertQuery
	ClientQuery
	EndpointQuery
	LinkQuery
	NetworkQuery
	ProxyQuery
	ServerQuery
	SMTPSettingsQuery
	StatsQuery
	UserQuery
	WireGuardQuery
	WorkerQuery
}

type Mutation interface {
	CertMutation
	ClientMutation
	EndpointMutation
	LinkMutation
	NetworkMutation
	ProxyMutation
	ServerMutation
	SMTPSettingsMutation
	StatsMutation
	UserMutation
	WireGuardMutation
	WorkerMutation
	UserGroupMutation
}

// queryImpl/mutationImpl 是具体表级实现的基础结构（持有 ctx）。
type queryImpl struct {
	ctx *app.Context
}

type mutationImpl struct {
	ctx *app.Context
}

// compositeQuery / compositeMutation 组合各子领域实现，对外暴露统一入口。
type compositeQuery struct {
	CertQuery
	ClientQuery
	EndpointQuery
	LinkQuery
	NetworkQuery
	ProxyQuery
	ServerQuery
	SMTPSettingsQuery
	StatsQuery
	UserQuery
	WireGuardQuery
	WorkerQuery
}

type compositeMutation struct {
	CertMutation
	ClientMutation
	EndpointMutation
	LinkMutation
	NetworkMutation
	ProxyMutation
	ServerMutation
	SMTPSettingsMutation
	StatsMutation
	UserMutation
	WireGuardMutation
	WorkerMutation
	UserGroupMutation
}

func NewQuery(ctx *app.Context) Query {
	base := &queryImpl{ctx: ctx}
	return &compositeQuery{
		CertQuery:      newCertQuery(base),
		ClientQuery:    newClientQuery(base),
		EndpointQuery:  newEndpointQuery(base),
		LinkQuery:      newLinkQuery(base),
		NetworkQuery:   newNetworkQuery(base),
		ProxyQuery:     newProxyQuery(base),
		ServerQuery:    newServerQuery(base),
		SMTPSettingsQuery: newSMTPSettingsQuery(base),
		StatsQuery:     newStatsQuery(base),
		UserQuery:      newUserQuery(base),
		WireGuardQuery: newWireGuardQuery(base),
		WorkerQuery:    newWorkerQuery(base),
	}
}

func NewMutation(ctx *app.Context) Mutation {
	base := &mutationImpl{ctx: ctx}
	return &compositeMutation{
		CertMutation:      newCertMutation(base),
		ClientMutation:    newClientMutation(base),
		EndpointMutation:  newEndpointMutation(base),
		LinkMutation:      newLinkMutation(base),
		NetworkMutation:   newNetworkMutation(base),
		ProxyMutation:     newProxyMutation(base),
		ServerMutation:    newServerMutation(base),
		SMTPSettingsMutation: newSMTPSettingsMutation(base),
		StatsMutation:     newStatsMutation(base),
		UserMutation:      newUserMutation(base),
		WireGuardMutation: newWireGuardMutation(base),
		WorkerMutation:    newWorkerMutation(base),
		UserGroupMutation: newUserGroupMutation(base),
	}
}

var (
	_ Query    = (*compositeQuery)(nil)
	_ Mutation = (*compositeMutation)(nil)
)

// customSortOrderExpr 自定义排序的统一排序规则（客户端/服务端列表共用）：
// 1. 未设置序号（sort_index = 0）的记录排在最后，保证新增节点默认追加在列表尾部；
// 2. 已设置的按 sort_index 升序，即序号数字越小越靠前；
// 3. 序号相同时按创建时间升序，保证顺序稳定（升级前无序号时与原有默认顺序一致）。
const customSortOrderExpr = "(sort_index = 0) ASC, sort_index ASC, created_at ASC"
