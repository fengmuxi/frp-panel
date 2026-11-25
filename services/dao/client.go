package dao

import (
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

type ClientQuery interface {
	ValidateClientSecret(clientID, clientSecret string) (*models.ClientEntity, error)
	AdminGetClientByClientID(clientID string) (*models.Client, error)
	AdminGetAllClients() ([]*models.Client, error)
	AdminGetClientsByServerID(serverID string) ([]*models.Client, error)
	AdminGetClientsByOriginClientID(originClientID string) ([]*models.Client, error)
	GetClientByClientID(userInfo models.UserInfo, clientID string) (*models.Client, error)
	GetClientsByClientIDs(userInfo models.UserInfo, clientIDs []string) ([]*models.Client, error)
	GetClientByFilter(userInfo models.UserInfo, client *models.ClientEntity, shadow *bool) (*models.ClientEntity, error)
	GetClientByOriginClientID(originClientID string) (*models.ClientEntity, error)
	ListClients(userInfo models.UserInfo, page, pageSize int) ([]*models.ClientEntity, error)
	ListClientsWithKeyword(userInfo models.UserInfo, page, pageSize int, keyword string) ([]*models.ClientEntity, error)
	ListAllClientsOrdered(userInfo models.UserInfo) ([]*models.ClientEntity, error)
	GetAllClients(userInfo models.UserInfo) ([]*models.ClientEntity, error)
	CountClients(userInfo models.UserInfo) (int64, error)
	CountClientsWithKeyword(userInfo models.UserInfo, keyword string) (int64, error)
	CountClientsByComment(userInfo models.UserInfo, comment string, excludeClientIDs ...string) (int64, error)
	CountConfiguredClients(userInfo models.UserInfo) (int64, error)
	CountClientsInShadow(userInfo models.UserInfo, clientID string) (int64, error)
	GetClientIDsInShadowByClientID(userInfo models.UserInfo, clientID string) ([]string, error)
	AdminGetClientIDsInShadowByClientID(clientID string) ([]string, error)
}

type ClientMutation interface {
	CreateClient(userInfo models.UserInfo, client *models.ClientEntity) error
	DeleteClient(userInfo models.UserInfo, clientID string) error
	UpdateClient(userInfo models.UserInfo, client *models.ClientEntity) error
	UpdateClientSortOrders(userInfo models.UserInfo, orderedClientIDs []string) error
	AdminUpdateClientLastSeen(clientID string) error
}

type clientQuery struct{ *queryImpl }
type clientMutation struct{ *mutationImpl }

func newClientQuery(base *queryImpl) ClientQuery          { return &clientQuery{base} }
func newClientMutation(base *mutationImpl) ClientMutation { return &clientMutation{base} }

func (q *clientQuery) ValidateClientSecret(clientID, clientSecret string) (*models.ClientEntity, error) {
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("invalid client id or client secret")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	c := &models.Client{}
	err := db.
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			ClientID: clientID,
		}}).
		First(c).Error
	if err != nil {
		return nil, err
	}
	// 常量时间比较（安全审计 P2：避免逐字节时序泄露节点接入凭证）
	if subtle.ConstantTimeCompare([]byte(c.ConnectSecret), []byte(clientSecret)) != 1 {
		return nil, fmt.Errorf("invalid client secret")
	}
	return c.ClientEntity, nil
}

func (q *clientQuery) AdminGetClientByClientID(clientID string) (*models.Client, error) {
	if clientID == "" {
		return nil, fmt.Errorf("invalid client id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	c := &models.Client{}
	err := db.
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			ClientID: clientID,
		}}).
		First(c).Error
	if err != nil {
		return nil, err
	}
	return c, nil
}

// AdminGetClientsByServerID 返回连接指定节点的全部 client（不限归属用户），
// 供推送处理等服务端内部逻辑使用。
// AdminGetAllClients 返回全部客户端（不限归属用户），供周期对账等内部任务使用
func (q *clientQuery) AdminGetAllClients() ([]*models.Client, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	clients := []*models.Client{}
	if err := db.Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

func (q *clientQuery) AdminGetClientsByServerID(serverID string) ([]*models.Client, error) {
	if serverID == "" {
		return nil, fmt.Errorf("invalid server id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	clients := []*models.Client{}
	err := db.
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			ServerID: serverID,
		}}).
		Find(&clients).Error
	if err != nil {
		return nil, err
	}
	return clients, nil
}

// AdminGetClientsByOriginClientID 返回 origin client 的全部 child（含 Workers 关联预加载），
// 不限归属用户——级联删除等 server 级清理使用
func (q *clientQuery) AdminGetClientsByOriginClientID(originClientID string) ([]*models.Client, error) {
	if originClientID == "" {
		return nil, fmt.Errorf("invalid origin client id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	clients := []*models.Client{}
	err := db.Preload("Workers").
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			OriginClientID: originClientID,
		}}).
		Find(&clients).Error
	if err != nil {
		return nil, err
	}
	return clients, nil
}

func (q *clientQuery) GetClientByClientID(userInfo models.UserInfo, clientID string) (*models.Client, error) {
	if clientID == "" {
		return nil, fmt.Errorf("invalid client id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	c := &models.Client{}
	err := db.
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
			ClientID: clientID,
		}}).
		First(c).Error
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (q *clientQuery) GetClientsByClientIDs(userInfo models.UserInfo, clientIDs []string) ([]*models.Client, error) {
	if len(clientIDs) == 0 {
		return nil, fmt.Errorf("invalid client ids")
	}
	if userInfo == nil {
		return nil, fmt.Errorf("invalid user info")
	}

	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	cs := []*models.Client{}
	query := db.Where("client_id IN ?", clientIDs)
	// 修复安全审计 P0-3：此前不过滤租户，worker 可附加其他租户的 client 并部署代码。
	// admin 角色放行（跨租户运维），普通用户仅能命中自己的 client。
	if !userInfo.IsAdmin() {
		query = query.Where("user_id = ? AND tenant_id = ?", userInfo.GetUserID(), userInfo.GetTenantID())
	}
	err := query.Find(&cs).Error
	if err != nil {
		return nil, err
	}

	return cs, nil
}

func (q *clientQuery) GetClientByFilter(userInfo models.UserInfo, client *models.ClientEntity, shadow *bool) (*models.ClientEntity, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	filter := &models.ClientEntity{}
	if len(client.ClientID) != 0 {
		filter.ClientID = client.ClientID
	}
	if len(client.OriginClientID) != 0 {
		filter.OriginClientID = client.OriginClientID
	}
	if len(client.ConnectSecret) != 0 {
		filter.ConnectSecret = client.ConnectSecret
	}
	if len(client.ServerID) != 0 {
		filter.ServerID = client.ServerID
	}
	if shadow != nil {
		filter.IsShadow = *shadow
	}
	c := &models.Client{}

	err := db.
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		}}).
		Where(filter).
		First(c).Error
	if err != nil {
		return nil, err
	}
	return c.ClientEntity, nil
}

func (q *clientQuery) GetClientByOriginClientID(originClientID string) (*models.ClientEntity, error) {
	if originClientID == "" {
		return nil, fmt.Errorf("invalid origin client id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	c := &models.Client{}
	err := db.
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			OriginClientID: originClientID,
		}}).
		First(c).Error
	if err != nil {
		return nil, err
	}
	return c.ClientEntity, nil
}

func (m *clientMutation) CreateClient(userInfo models.UserInfo, client *models.ClientEntity) error {
	client.UserID = userInfo.GetUserID()
	client.TenantID = userInfo.GetTenantID()
	c := &models.Client{
		ClientEntity: client,
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Create(c).Error
}

func (m *clientMutation) DeleteClient(userInfo models.UserInfo, clientID string) error {
	if clientID == "" {
		return fmt.Errorf("invalid client id")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	if err := db.Unscoped().Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			ClientID: clientID,
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
	}).Or(&models.Client{
		ClientEntity: &models.ClientEntity{
			OriginClientID: clientID,
			UserID:         userInfo.GetUserID(),
			TenantID:       userInfo.GetTenantID(),
		},
	}).Delete(&models.Client{}).Error; err != nil {
		return err
	}
	// 级联清理该节点的通知覆盖配置，避免孤儿记录
	return deleteResourceNotifyPrefs(m.ctx, models.NotifyResTypeClient, clientID)
}

func (m *clientMutation) UpdateClient(userInfo models.UserInfo, client *models.ClientEntity) error {
	c := &models.Client{
		ClientEntity: client,
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
	}).Save(c).Error
}

// UpdateClientSortOrders 按给定顺序批量重排 sort_index（位置从 1 开始），供排序交换后全量回写。
func (m *clientMutation) UpdateClientSortOrders(userInfo models.UserInfo, orderedClientIDs []string) error {
	if len(orderedClientIDs) == 0 {
		return nil
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Transaction(func(tx *gorm.DB) error {
		for i, clientID := range orderedClientIDs {
			if err := tx.Model(&models.Client{}).
				Where("client_id = ? AND user_id = ? AND tenant_id = ?",
					clientID, userInfo.GetUserID(), userInfo.GetTenantID()).
				Update("sort_index", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (q *clientQuery) ListClients(userInfo models.UserInfo, page, pageSize int) ([]*models.ClientEntity, error) {
	if page < 1 || pageSize < 1 {
		return nil, fmt.Errorf("invalid page or page size")
	}

	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	offset := (page - 1) * pageSize

	var clients []*models.Client
	err := db.Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
	}).
		Where(
			db.Where(
				normalClientFilter(db),
			),
		).Order(customSortOrderExpr).Offset(offset).Limit(pageSize).Find(&clients).Error
	if err != nil {
		return nil, err
	}

	return lo.Map(clients, func(c *models.Client, _ int) *models.ClientEntity {
		return c.ClientEntity
	}), nil
}

func (q *clientQuery) ListClientsWithKeyword(userInfo models.UserInfo, page, pageSize int, keyword string) ([]*models.ClientEntity, error) {
	// 只获取没shadow且config有东西
	// 或isShadow的client
	if page < 1 || pageSize < 1 || len(keyword) == 0 {
		return nil, fmt.Errorf("invalid page or page size or keyword")
	}

	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	offset := (page - 1) * pageSize

	var clients []*models.Client
	// 界面以名称为主展示，关键词同时匹配 ID 与名称
	err := db.Where("(client_id like ? OR comment like ?)", "%"+keyword+"%", "%"+keyword+"%").
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		}}).
		Where(normalClientFilter(db)).
		Order(customSortOrderExpr).Offset(offset).Limit(pageSize).Find(&clients).Error
	if err != nil {
		return nil, err
	}

	return lo.Map(clients, func(c *models.Client, _ int) *models.ClientEntity {
		return c.ClientEntity
	}), nil
}

// ListAllClientsOrdered 返回用户全部可排序客户端（过滤条件与 ListClients 一致），
// 按自定义排序规则全量有序返回，用于交换排序时定位上/下邻居（跨分页场景）。
func (q *clientQuery) ListAllClientsOrdered(userInfo models.UserInfo) ([]*models.ClientEntity, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var clients []*models.Client
	err := db.Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
	}).
		Where(
			db.Where(
				normalClientFilter(db),
			),
		).Order(customSortOrderExpr).Find(&clients).Error
	if err != nil {
		return nil, err
	}

	return lo.Map(clients, func(c *models.Client, _ int) *models.ClientEntity {
		return c.ClientEntity
	}), nil
}

func (q *clientQuery) GetAllClients(userInfo models.UserInfo) ([]*models.ClientEntity, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var clients []*models.Client
	err := db.Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
	}).Find(&clients).Error
	if err != nil {
		return nil, err
	}

	return lo.Map(clients, func(c *models.Client, _ int) *models.ClientEntity {
		return c.ClientEntity
	}), nil
}

func (q *clientQuery) CountClients(userInfo models.UserInfo) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	err := db.Model(&models.Client{}).Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
	}).
		Where(normalClientFilter(db)).Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// CountClientsByComment 统计同一用户可见范围内名称（comment）相同的客户端数量，
// 用于名称唯一性校验；excludeClientIDs 用于改名时排除自身（可同时排除影子本体与
// 其子副本，二者 comment 相同），空名称不参与查重。
func (q *clientQuery) CountClientsByComment(userInfo models.UserInfo, comment string, excludeClientIDs ...string) (int64, error) {
	if len(comment) == 0 {
		return 0, nil
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	query := db.Model(&models.Client{}).
		Where(&models.Client{ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		}}).
		Where("comment = ?", comment).
		Where(normalClientFilter(db))
	exclude := lo.Filter(excludeClientIDs, func(id string, _ int) bool { return len(id) > 0 })
	if len(exclude) > 0 {
		query = query.Where("client_id NOT IN ?", exclude)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (q *clientQuery) CountClientsWithKeyword(userInfo models.UserInfo, keyword string) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	err := db.Model(&models.Client{}).Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			UserID:   userInfo.GetUserID(),
			TenantID: userInfo.GetTenantID(),
		},
	}).
		Where(normalClientFilter(db)).Where("(client_id like ? OR comment like ?)", "%"+keyword+"%", "%"+keyword+"%").Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (q *clientQuery) CountConfiguredClients(userInfo models.UserInfo) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	err := db.Model(&models.Client{}).
		Where(&models.Client{
			ClientEntity: &models.ClientEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
			}}).
		Where(normalClientFilter(db)).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (q *clientQuery) CountClientsInShadow(userInfo models.UserInfo, clientID string) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	err := db.Model(&models.Client{}).
		Where(&models.Client{
			ClientEntity: &models.ClientEntity{
				UserID:         userInfo.GetUserID(),
				TenantID:       userInfo.GetTenantID(),
				OriginClientID: clientID,
			}}).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (q *clientQuery) GetClientIDsInShadowByClientID(userInfo models.UserInfo, clientID string) ([]string, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var clients []*models.Client
	err := db.Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			UserID:         userInfo.GetUserID(),
			TenantID:       userInfo.GetTenantID(),
			OriginClientID: clientID,
		}}).Find(&clients).Error
	if err != nil {
		return nil, err
	}
	return lo.Map(clients, func(c *models.Client, _ int) string {
		return c.ClientID
	}), nil
}

func (q *clientQuery) AdminGetClientIDsInShadowByClientID(clientID string) ([]string, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var clients []*models.Client
	err := db.Where(&models.Client{
		ClientEntity: &models.ClientEntity{
			OriginClientID: clientID,
		}}).Find(&clients).Error
	if err != nil {
		return nil, err
	}
	return lo.Map(clients, func(c *models.Client, _ int) string {
		return c.ClientID
	}), nil
}

func (m *clientMutation) AdminUpdateClientLastSeen(clientID string) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Model(&models.Client{
		ClientEntity: &models.ClientEntity{
			ClientID: clientID,
		}}).Update("last_seen_at", time.Now()).Error
}

func normalClientFilter(db *gorm.DB) *gorm.DB {
	// 1. 没shadow过的老client
	// 2. shadow过的shadow client
	// 3. 非临时节点
	return db.Where(
		db.Where("origin_client_id is NULL").
			Or("is_shadow = ?", true).
			Or("LENGTH(origin_client_id) = ?", 0),
	).
		Where(
			db.Where(
				db.Where("ephemeral is NULL").
					Or("ephemeral = ?", false),
			).Or(
				db.Where("ephemeral = ?", true).
					Where("last_seen_at > ?", time.Now().Add(-5*time.Minute)),
			),
		)
}
