package dao

import (
	"crypto/subtle"
	"fmt"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/models"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

type ServerQuery interface {
	GetDefaultServer() (*models.ServerEntity, error)
	AdminGetAllServers() ([]*models.Server, error)
	ValidateServerSecret(serverID string, secret string) (*models.ServerEntity, error)
	AdminGetServerByServerID(serverID string) (*models.ServerEntity, error)
	GetServerByServerID(userInfo models.UserInfo, serverID string) (*models.ServerEntity, error)
	ListServers(userInfo models.UserInfo, page, pageSize int) ([]*models.ServerEntity, error)
	ListServersWithKeyword(userInfo models.UserInfo, page, pageSize int, keyword string) ([]*models.ServerEntity, error)
	ListAllServersOrdered(userInfo models.UserInfo) ([]*models.ServerEntity, error)
	CountServers(userInfo models.UserInfo) (int64, error)
	CountServersWithKeyword(userInfo models.UserInfo, keyword string) (int64, error)
	CountServersByComment(userInfo models.UserInfo, comment string, excludeServerID string) (int64, error)
	CountConfiguredServers(userInfo models.UserInfo) (int64, error)
}

type ServerMutation interface {
	InitDefaultServer(serverIP string)
	UpdateDefaultServer(c *models.Server) error
	CreateServer(userInfo models.UserInfo, server *models.ServerEntity) error
	DeleteServer(userInfo models.UserInfo, serverID string) error
	UpdateServer(userInfo models.UserInfo, server *models.ServerEntity) error
	UpdateServerSortOrders(userInfo models.UserInfo, orderedServerIDs []string) error
}

type serverQuery struct{ *queryImpl }
type serverMutation struct{ *mutationImpl }

func newServerQuery(base *queryImpl) ServerQuery          { return &serverQuery{base} }
func newServerMutation(base *mutationImpl) ServerMutation { return &serverMutation{base} }

func (m *serverMutation) InitDefaultServer(serverIP string) {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()

	// 默认服务端归属管理员：此前创建时未设置 user_id（为 0），
	// 首页/服务端列表按用户过滤后不可见，用户无法看到和配置它，
	// 导致 master 内置 frps 因配置为空而无法启动、隧道流量无从上报。
	adminTenantID := 0
	admin := models.User{}
	if err := db.Where("user_id = ?", defs.DefaultAdminUserID).First(&admin).Error; err == nil {
		adminTenantID = admin.TenantID
	}

	db.Where(&models.Server{
		ServerEntity: &models.ServerEntity{
			ServerID: defs.DefaultServerID,
		},
	}).Attrs(&models.Server{
		ServerEntity: &models.ServerEntity{
			ServerID:      defs.DefaultServerID,
			ServerIP:      serverIP,
			ConnectSecret: uuid.New().String(),
			Comment:       defs.DefaultServerName,
			UserID:        defs.DefaultAdminUserID,
			TenantID:      adminTenantID,
		},
	}).FirstOrCreate(&models.Server{})
	// 存量数据兜底：默认服务端名称为空时补写默认名称
	db.Model(&models.Server{}).
		Where("server_id = ? AND (comment IS NULL OR comment = '')", defs.DefaultServerID).
		Update("comment", defs.DefaultServerName)
	// 存量数据兜底：历史版本创建的默认服务端无归属（user_id=0），归属到管理员
	db.Model(&models.Server{}).
		Where("server_id = ? AND user_id = 0", defs.DefaultServerID).
		Updates(map[string]interface{}{"user_id": defs.DefaultAdminUserID, "tenant_id": adminTenantID})
}

func (q *serverQuery) GetDefaultServer() (*models.ServerEntity, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	c := &models.Server{}
	err := db.
		Where(&models.Server{ServerEntity: &models.ServerEntity{
			ServerID: defs.DefaultServerID,
		}}).
		First(c).Error
	if err != nil {
		return nil, err
	}
	return c.ServerEntity, nil
}

func (m *serverMutation) UpdateDefaultServer(c *models.Server) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	c.ServerID = defs.DefaultServerID
	err := db.Where(&models.Server{
		ServerEntity: &models.ServerEntity{
			ServerID: defs.DefaultServerID,
		}}).Save(c).Error
	if err != nil {
		return err
	}
	return nil
}

// AdminGetAllServers 返回全部服务端节点（不限归属用户），供周期对账等内部任务使用
func (q *serverQuery) AdminGetAllServers() ([]*models.Server, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.Server{}
	if err := db.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *serverQuery) ValidateServerSecret(serverID string, secret string) (*models.ServerEntity, error) {
	if serverID == "" || secret == "" {
		return nil, fmt.Errorf("invalid request")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	c := &models.Server{}
	err := db.
		Where(&models.Server{ServerEntity: &models.ServerEntity{
			ServerID: serverID,
		}}).
		First(c).Error
	if err != nil {
		return nil, err
	}
	// 常量时间比较（安全审计 P2）
	if subtle.ConstantTimeCompare([]byte(c.ConnectSecret), []byte(secret)) != 1 {
		return nil, fmt.Errorf("invalid secret")
	}
	return c.ServerEntity, nil
}

func (q *serverQuery) AdminGetServerByServerID(serverID string) (*models.ServerEntity, error) {
	if serverID == "" {
		return nil, fmt.Errorf("invalid server id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	c := &models.Server{}
	err := db.
		Where(&models.Server{ServerEntity: &models.ServerEntity{
			ServerID: serverID,
		}}).
		First(c).Error
	if err != nil {
		return nil, err
	}
	return c.ServerEntity, nil
}

func (q *serverQuery) GetServerByServerID(userInfo models.UserInfo, serverID string) (*models.ServerEntity, error) {
	if serverID == "" {
		return nil, fmt.Errorf("invalid server id")
	}
	if userInfo.GetUserID() == defs.DefaultAdminUserID && serverID == defs.DefaultServerID {
		return q.GetDefaultServer()
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	c := &models.Server{}
	err := db.
		Where(&models.Server{ServerEntity: &models.ServerEntity{
			TenantID: userInfo.GetTenantID(),
			UserID:   userInfo.GetUserID(),
			ServerID: serverID,
		}}).
		First(c).Error
	if err != nil {
		return nil, err
	}
	return c.ServerEntity, nil
}

func (m *serverMutation) CreateServer(userInfo models.UserInfo, server *models.ServerEntity) error {
	server.UserID = userInfo.GetUserID()
	server.TenantID = userInfo.GetTenantID()
	c := &models.Server{
		ServerEntity: server,
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Create(c).Error
}

func (m *serverMutation) DeleteServer(userInfo models.UserInfo, serverID string) error {
	if serverID == "" {
		return fmt.Errorf("invalid server id")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	if err := db.Unscoped().Where(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				TenantID: userInfo.GetTenantID(),
				UserID:   userInfo.GetUserID(),
			},
		},
	).Delete(&models.Server{
		ServerEntity: &models.ServerEntity{
			ServerID: serverID,
		},
	}).Error; err != nil {
		return err
	}
	// 级联清理该节点的通知覆盖配置，避免孤儿记录
	return deleteResourceNotifyPrefs(m.ctx, models.NotifyResTypeServer, serverID)
}

func (m *serverMutation) UpdateServer(userInfo models.UserInfo, server *models.ServerEntity) error {
	c := &models.Server{
		ServerEntity: server,
	}
	if userInfo.GetUserID() == defs.DefaultAdminUserID && server.ServerID == defs.DefaultServerID {
		return m.UpdateDefaultServer(c)
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Where(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
			},
		},
	).Save(c).Error
}

// UpdateServerSortOrders 按给定顺序批量重排 sort_index（位置从 1 开始），供排序交换后全量回写。
func (m *serverMutation) UpdateServerSortOrders(userInfo models.UserInfo, orderedServerIDs []string) error {
	if len(orderedServerIDs) == 0 {
		return nil
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Transaction(func(tx *gorm.DB) error {
		for i, serverID := range orderedServerIDs {
			// 默认服务器记录不属于任何用户（user_id/tenant_id 为 0），单独放行
			if err := tx.Model(&models.Server{}).
				Where("server_id = ? AND ((user_id = ? AND tenant_id = ?) OR server_id = ?)",
					serverID, userInfo.GetUserID(), userInfo.GetTenantID(), defs.DefaultServerID).
				Update("sort_index", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (q *serverQuery) ListServers(userInfo models.UserInfo, page, pageSize int) ([]*models.ServerEntity, error) {
	if page < 1 || pageSize < 1 {
		return nil, fmt.Errorf("invalid page or page size")
	}

	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	offset := (page - 1) * pageSize

	var servers []*models.Server
	err := db.Where(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
			},
		},
	).Or(&models.Server{
		ServerEntity: &models.ServerEntity{
			ServerID: defs.DefaultServerID,
		},
	}).Order(customSortOrderExpr).Offset(offset).Limit(pageSize).Find(&servers).Error
	if err != nil {
		return nil, err
	}

	return lo.Map(servers, func(c *models.Server, _ int) *models.ServerEntity {
		return c.ServerEntity
	}), nil
}

// ListAllServersOrdered 返回全部可见服务端（过滤条件与 ListServers 一致），
// 按自定义排序规则全量有序返回，用于交换排序时定位上/下邻居（跨分页场景）。
func (q *serverQuery) ListAllServersOrdered(userInfo models.UserInfo) ([]*models.ServerEntity, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var servers []*models.Server
	err := db.Where(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
			},
		},
	).Or(&models.Server{
		ServerEntity: &models.ServerEntity{
			ServerID: defs.DefaultServerID,
		},
	}).Order(customSortOrderExpr).Find(&servers).Error
	if err != nil {
		return nil, err
	}

	return lo.Map(servers, func(c *models.Server, _ int) *models.ServerEntity {
		return c.ServerEntity
	}), nil
}

func (q *serverQuery) ListServersWithKeyword(userInfo models.UserInfo, page, pageSize int, keyword string) ([]*models.ServerEntity, error) {
	if page < 1 || pageSize < 1 || len(keyword) == 0 {
		return nil, fmt.Errorf("invalid page or page size or keyword")
	}

	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	offset := (page - 1) * pageSize

	var servers []*models.Server
	err := db.Where(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
			},
		},
	).Where("(server_id like ? OR comment like ?)", "%"+keyword+"%", "%"+keyword+"%").
		Order(customSortOrderExpr).Offset(offset).Limit(pageSize).Find(&servers).Error
	if err != nil {
		return nil, err
	}

	return lo.Map(servers, func(c *models.Server, _ int) *models.ServerEntity {
		return c.ServerEntity
	}), nil
}

func (q *serverQuery) CountServers(userInfo models.UserInfo) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	err := db.Model(&models.Server{}).Where(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
			},
		},
	).Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// CountServersByComment 统计用户可见范围内名称（comment）相同的服务端数量，
// 用于名称唯一性校验；默认服务端（server_id=default）归属 user_id=0，单独放行参与查重，
// excludeServerID 用于改名时排除自身，空名称不参与查重。
func (q *serverQuery) CountServersByComment(userInfo models.UserInfo, comment string, excludeServerID string) (int64, error) {
	if len(comment) == 0 {
		return 0, nil
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	err := db.Model(&models.Server{}).
		Where(db.Where(
			&models.Server{
				ServerEntity: &models.ServerEntity{
					UserID:   userInfo.GetUserID(),
					TenantID: userInfo.GetTenantID(),
				},
			},
		).Or(&models.Server{
			ServerEntity: &models.ServerEntity{
				ServerID: defs.DefaultServerID,
			},
		})).
		Where("comment = ?", comment).
		Where("server_id != ?", excludeServerID).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (q *serverQuery) CountServersWithKeyword(userInfo models.UserInfo, keyword string) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	err := db.Model(&models.Server{}).Where(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
			},
		},
	).Where("(server_id like ? OR comment like ?)", "%"+keyword+"%", "%"+keyword+"%").Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (q *serverQuery) CountConfiguredServers(userInfo models.UserInfo) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	err := db.Model(&models.Server{}).Where(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				UserID:   userInfo.GetUserID(),
				TenantID: userInfo.GetTenantID(),
			},
		},
	).Not(
		&models.Server{
			ServerEntity: &models.ServerEntity{
				ConfigContent: []byte{},
			},
		},
	).Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}
