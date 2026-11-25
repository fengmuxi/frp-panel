package dao

import (
	"fmt"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"gorm.io/gorm"
)

// endpoint.go endpoints 的查询与变更。
//
// 安全审计 P1-6：此前 endpoint 的 CRUD/列表完全不带租户过滤（按裸 ID 查、列表全表），
// 任意租户可读/改/删其他租户的组网端点。现在所有方法都按调用者身份收敛：
// 普通用户只见 user_id/tenant_id 匹配的行，admin 放行全量；
// 归属列由迁移 002 按 client_id 回填，新建时从关联的 client 解析写入。

type EndpointQuery interface {
	GetEndpointByID(userInfo models.UserInfo, id uint) (*models.Endpoint, error)
	AdminGetEndpointsByClientIDs(clientIDs []string) ([]*models.Endpoint, error)
	ListEndpoints(userInfo models.UserInfo, page, pageSize int) ([]*models.Endpoint, error)
	CountEndpoints(userInfo models.UserInfo) (int64, error)
	ListEndpointsWithFilters(userInfo models.UserInfo, page, pageSize int, clientID string, wireguardID uint, keyword string) ([]*models.Endpoint, error)
	CountEndpointsWithFilters(userInfo models.UserInfo, clientID string, wireguardID uint, keyword string) (int64, error)
}

type EndpointMutation interface {
	CreateEndpoint(userInfo models.UserInfo, endpoint *models.EndpointEntity) error
	UpdateEndpoint(userInfo models.UserInfo, id uint, endpoint *models.EndpointEntity) error
	DeleteEndpoint(userInfo models.UserInfo, id uint) error
}

type endpointQuery struct{ *queryImpl }
type endpointMutation struct{ *mutationImpl }

func newEndpointQuery(base *queryImpl) EndpointQuery          { return &endpointQuery{base} }
func newEndpointMutation(base *mutationImpl) EndpointMutation { return &endpointMutation{base} }

// applyEndpointScope 给查询追加调用者身份过滤；admin 放行全量，nil 用户不可见。
func applyEndpointScope(db *gorm.DB, userInfo models.UserInfo) *gorm.DB {
	if userInfo != nil && userInfo.IsAdmin() {
		return db
	}
	if userInfo == nil {
		return db.Where("1 = 0")
	}
	return db.Where("user_id = ? AND tenant_id = ?", userInfo.GetUserID(), userInfo.GetTenantID())
}

// resolveClientOwnership 解析 client 的归属（user_id/tenant_id）。
// 普通用户只能解析自己的 client；admin 可解析任意 client。
func resolveClientOwnership(ctx *app.Context, userInfo models.UserInfo, clientID string) (int, int, error) {
	if clientID == "" {
		if userInfo == nil {
			return 0, 0, fmt.Errorf("no user info and no client id")
		}
		return userInfo.GetUserID(), userInfo.GetTenantID(), nil
	}
	db := ctx.GetApp().GetDBManager().GetDefaultDB()
	var c models.Client
	q := db.Unscoped().Where("client_id = ?", clientID)
	if userInfo == nil {
		return 0, 0, fmt.Errorf("client [%s] not accessible: no user info", clientID)
	}
	if !userInfo.IsAdmin() {
		q = q.Where("user_id = ? AND tenant_id = ?", userInfo.GetUserID(), userInfo.GetTenantID())
	}
	if err := q.First(&c).Error; err != nil || c.ClientEntity == nil {
		return 0, 0, fmt.Errorf("client [%s] not accessible for current user", clientID)
	}
	return c.UserID, c.TenantID, nil
}

func (m *endpointMutation) CreateEndpoint(userInfo models.UserInfo, endpoint *models.EndpointEntity) error {
	if endpoint == nil {
		return fmt.Errorf("invalid endpoint entity")
	}
	if len(endpoint.Host) == 0 || endpoint.Port == 0 {
		return fmt.Errorf("invalid endpoint host or port")
	}

	// 归属：从关联的 client 解析（同时完成归属校验——别人的 clientID 会被拒绝）
	userID, tenantID, err := resolveClientOwnership(m.ctx, userInfo, endpoint.ClientID)
	if err != nil {
		return err
	}
	endpoint.UserID = userID
	endpoint.TenantID = tenantID

	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Create(&models.Endpoint{EndpointEntity: endpoint}).Error
}

func (m *endpointMutation) UpdateEndpoint(userInfo models.UserInfo, id uint, endpoint *models.EndpointEntity) error {
	if id == 0 || endpoint == nil {
		return fmt.Errorf("invalid endpoint id or entity")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()

	var existing models.Endpoint
	if err := applyEndpointScope(db, userInfo).Where("id = ?", id).First(&existing).Error; err != nil {
		return fmt.Errorf("endpoint [%d] not accessible for current user", id)
	}

	// 不允许通过更新改变归属：保留已加载行的归属列
	endpoint.UserID = existing.UserID
	endpoint.TenantID = existing.TenantID
	existing.EndpointEntity = endpoint
	return db.Save(&existing).Error
}

func (m *endpointMutation) DeleteEndpoint(userInfo models.UserInfo, id uint) error {
	if id == 0 {
		return fmt.Errorf("invalid endpoint id")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return applyEndpointScope(db, userInfo).Where("id = ?", id).
		Unscoped().Delete(&models.Endpoint{}).Error
}

// AdminGetEndpointsByClientIDs 返回挂载在给定 client 集合上的全部 Endpoint，
// 不限归属用户——级联删除等 server 级清理使用
func (q *endpointQuery) AdminGetEndpointsByClientIDs(clientIDs []string) ([]*models.Endpoint, error) {
	if len(clientIDs) == 0 {
		return []*models.Endpoint{}, nil
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.Endpoint{}
	if err := db.Where("client_id IN ?", clientIDs).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *endpointQuery) GetEndpointByID(userInfo models.UserInfo, id uint) (*models.Endpoint, error) {
	if id == 0 {
		return nil, fmt.Errorf("invalid endpoint id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var e models.Endpoint
	if err := applyEndpointScope(db, userInfo).Where("id = ?", id).First(&e).Error; err != nil {
		return nil, fmt.Errorf("endpoint [%d] not accessible for current user", id)
	}
	return &e, nil
}

func (q *endpointQuery) ListEndpoints(userInfo models.UserInfo, page, pageSize int) ([]*models.Endpoint, error) {
	if page < 1 || pageSize < 1 {
		return nil, fmt.Errorf("invalid page or page size")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var list []*models.Endpoint
	offset := (page - 1) * pageSize
	if err := applyEndpointScope(db, userInfo).Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *endpointQuery) CountEndpoints(userInfo models.UserInfo) (int64, error) {
	var count int64
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	if err := applyEndpointScope(db, userInfo).Model(&models.Endpoint{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ListEndpointsWithFilters 根据 clientID / wireguardID / keyword 过滤端点
func (q *endpointQuery) ListEndpointsWithFilters(userInfo models.UserInfo, page, pageSize int, clientID string, wireguardID uint, keyword string) ([]*models.Endpoint, error) {
	if page < 1 || pageSize < 1 {
		return nil, fmt.Errorf("invalid page or page size")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()

	// 若指定 clientID，校验归属
	if len(clientID) > 0 {
		if _, err := newClientQuery(q.queryImpl).GetClientByClientID(userInfo, clientID); err != nil {
			return nil, err
		}
	}
	// 若指定 wireguardID，先校验该 wireguard 归属（安全审计 P1-6：原先直接按 ID 过滤）
	if wireguardID > 0 {
		if _, err := newWireGuardQuery(q.queryImpl).GetWireGuardByID(userInfo, wireguardID); err != nil {
			return nil, fmt.Errorf("wireguard [%d] not accessible for current user", wireguardID)
		}
	}

	var list []*models.Endpoint
	offset := (page - 1) * pageSize
	query := applyEndpointScope(db.Model(&models.Endpoint{}), userInfo)
	if len(clientID) > 0 {
		query = query.Where(&models.Endpoint{EndpointEntity: &models.EndpointEntity{ClientID: clientID}})
	}
	if wireguardID > 0 {
		query = query.Where(&models.Endpoint{EndpointEntity: &models.EndpointEntity{WireGuardID: wireguardID}})
	}
	if len(keyword) > 0 {
		query = query.Where("host like ?", "%"+keyword+"%")
	}
	if err := query.Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *endpointQuery) CountEndpointsWithFilters(userInfo models.UserInfo, clientID string, wireguardID uint, keyword string) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()

	if len(clientID) > 0 {
		if _, err := newClientQuery(q.queryImpl).GetClientByClientID(userInfo, clientID); err != nil {
			return 0, err
		}
	}
	if wireguardID > 0 {
		if _, err := newWireGuardQuery(q.queryImpl).GetWireGuardByID(userInfo, wireguardID); err != nil {
			return 0, fmt.Errorf("wireguard [%d] not accessible for current user", wireguardID)
		}
	}

	var count int64
	query := applyEndpointScope(db.Model(&models.Endpoint{}), userInfo)
	if len(clientID) > 0 {
		query = query.Where(&models.Endpoint{EndpointEntity: &models.EndpointEntity{ClientID: clientID}})
	}
	if wireguardID > 0 {
		query = query.Where(&models.Endpoint{EndpointEntity: &models.EndpointEntity{WireGuardID: wireguardID}})
	}
	if len(keyword) > 0 {
		query = query.Where("host like ?", "%"+keyword+"%")
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
