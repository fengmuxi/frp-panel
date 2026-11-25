package dao

import (
	"fmt"

	"github.com/fengmuxi/frp-panel/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WireGuardQuery interface {
	GetWireGuardByID(userInfo models.UserInfo, id uint) (*models.WireGuard, error)
	AdminGetWireGuardByClientIDAndInterfaceName(clientID, interfaceName string) (*models.WireGuard, error)
	GetWireGuardsByNetworkID(userInfo models.UserInfo, networkID uint) ([]*models.WireGuard, error)
	AdminGetWireGuardsByClientIDs(clientIDs []string) ([]*models.WireGuard, error)
	GetWireGuardLocalAddressesByNetworkID(userInfo models.UserInfo, networkID uint) ([]string, error)
	ListWireGuardsWithFilters(userInfo models.UserInfo, page, pageSize int, filter *models.WireGuardEntity, keyword string) ([]*models.WireGuard, error)
	AdminListWireGuardsWithClientID(clientID string) ([]*models.WireGuard, error)
	AdminListWireGuardsWithNetworkIDs(networkIDs []uint) ([]*models.WireGuard, error)
	CountWireGuardsWithFilters(userInfo models.UserInfo, filter *models.WireGuardEntity, keyword string) (int64, error)
}

type WireGuardMutation interface {
	CreateWireGuard(userInfo models.UserInfo, wg *models.WireGuard) error
	UpdateWireGuard(userInfo models.UserInfo, id uint, wg *models.WireGuard) error
	DeleteWireGuard(userInfo models.UserInfo, id uint) error
}

type wireGuardQuery struct{ *queryImpl }
type wireGuardMutation struct{ *mutationImpl }

func newWireGuardQuery(base *queryImpl) WireGuardQuery          { return &wireGuardQuery{base} }
func newWireGuardMutation(base *mutationImpl) WireGuardMutation { return &wireGuardMutation{base} }

func (m *wireGuardMutation) CreateWireGuard(userInfo models.UserInfo, wg *models.WireGuard) error {
	if wg == nil || wg.WireGuardEntity == nil {
		return fmt.Errorf("invalid wireguard entity")
	}
	if len(wg.Name) == 0 || len(wg.PrivateKey) == 0 || len(wg.LocalAddress) == 0 {
		return fmt.Errorf("invalid wireguard fields")
	}

	wg.UserId = uint32(userInfo.GetUserID())
	wg.TenantId = uint32(userInfo.GetTenantID())

	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	// Omit 关联：advertised endpoints 由 handler 统一创建（此前双创建撞唯一索引）
	return db.Omit(clause.Associations).Create(wg).Error
}

func (m *wireGuardMutation) UpdateWireGuard(userInfo models.UserInfo, id uint, wg *models.WireGuard) error {
	if id == 0 || wg == nil || wg.WireGuardEntity == nil {
		return fmt.Errorf("invalid wireguard id or entity")
	}

	wg.UserId = uint32(userInfo.GetUserID())
	wg.TenantId = uint32(userInfo.GetTenantID())

	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()

	// 不触碰 endpoints 关联：端点绑定由 handler 的 bind/unbind 逻辑按 WireGuardID
	// 管理（此前 Unscoped().Clear() 会硬删 endpoint 行，FK 生效的库还会级联摧毁
	// wireguard_links）；Omit 关联避免 Save 顺带覆写端点
	wg.Model = gorm.Model{ID: id}
	return db.Omit(clause.Associations).
		Where(&models.WireGuard{Model: gorm.Model{ID: id}, WireGuardEntity: &models.WireGuardEntity{
			UserId:   uint32(userInfo.GetUserID()),
			TenantId: uint32(userInfo.GetTenantID()),
		}}).Save(wg).Error
}

func (m *wireGuardMutation) DeleteWireGuard(userInfo models.UserInfo, id uint) error {
	if id == 0 {
		return fmt.Errorf("invalid wireguard id")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Unscoped().Where(&models.WireGuard{
		Model: gorm.Model{ID: id},
		WireGuardEntity: &models.WireGuardEntity{
			UserId:   uint32(userInfo.GetUserID()),
			TenantId: uint32(userInfo.GetTenantID()),
		},
	}).Delete(&models.WireGuard{}).Error
}

func (q *wireGuardQuery) GetWireGuardByID(userInfo models.UserInfo, id uint) (*models.WireGuard, error) {
	if id == 0 {
		return nil, fmt.Errorf("invalid wireguard id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var m models.WireGuard
	if err := db.
		Preload("AdvertisedEndpoints").
		Preload("Network").
		Where(&models.WireGuard{
			Model: gorm.Model{ID: id},
			WireGuardEntity: &models.WireGuardEntity{
				UserId:   uint32(userInfo.GetUserID()),
				TenantId: uint32(userInfo.GetTenantID()),
			},
		}).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (q *wireGuardQuery) AdminGetWireGuardByClientIDAndInterfaceName(clientID, interfaceName string) (*models.WireGuard, error) {
	if clientID == "" || interfaceName == "" {
		return nil, fmt.Errorf("invalid client id or interface name")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var m models.WireGuard

	if err := db.Where(&models.WireGuard{WireGuardEntity: &models.WireGuardEntity{
		ClientID: clientID,
		Name:     interfaceName,
	}}).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (q *wireGuardQuery) GetWireGuardsByNetworkID(userInfo models.UserInfo, networkID uint) ([]*models.WireGuard, error) {
	if networkID == 0 {
		return nil, fmt.Errorf("invalid network id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var list []*models.WireGuard
	if err := db.Preload("Network").
		Preload("AdvertisedEndpoints").
		Where(&models.WireGuard{WireGuardEntity: &models.WireGuardEntity{
			UserId:    uint32(userInfo.GetUserID()),
			TenantId:  uint32(userInfo.GetTenantID()),
			NetworkID: networkID,
		}}).
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// AdminGetWireGuardsByClientIDs 返回挂载在给定 client 集合上的全部 WireGuard，
// 不限归属用户——级联删除等 server 级清理使用
func (q *wireGuardQuery) AdminGetWireGuardsByClientIDs(clientIDs []string) ([]*models.WireGuard, error) {
	if len(clientIDs) == 0 {
		return []*models.WireGuard{}, nil
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	list := []*models.WireGuard{}
	if err := db.Where("client_id IN ?", clientIDs).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *wireGuardQuery) GetWireGuardLocalAddressesByNetworkID(userInfo models.UserInfo, networkID uint) ([]string, error) {
	if networkID == 0 {
		return nil, fmt.Errorf("invalid network id")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var list []string
	if err := db.Model(&models.WireGuard{}).Where(&models.WireGuard{WireGuardEntity: &models.WireGuardEntity{
		UserId:    uint32(userInfo.GetUserID()),
		TenantId:  uint32(userInfo.GetTenantID()),
		NetworkID: networkID,
	}}).Pluck("local_address", &list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *wireGuardQuery) ListWireGuardsWithFilters(userInfo models.UserInfo, page, pageSize int, filter *models.WireGuardEntity, keyword string) ([]*models.WireGuard, error) {
	if page < 1 || pageSize < 1 {
		return nil, fmt.Errorf("invalid page or page size")
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var list []*models.WireGuard
	offset := (page - 1) * pageSize

	base := db.Preload("AdvertisedEndpoints").Where(&models.WireGuard{WireGuardEntity: &models.WireGuardEntity{
		UserId:   uint32(userInfo.GetUserID()),
		TenantId: uint32(userInfo.GetTenantID()),
	}})

	scoped := base
	if filter != nil {
		// only apply selected fields to filter
		f := &models.WireGuardEntity{}
		if len(filter.ClientID) > 0 {
			f.ClientID = filter.ClientID
		}
		if filter.NetworkID != 0 {
			f.NetworkID = filter.NetworkID
		}
		scoped = scoped.Where(&models.WireGuard{WireGuardEntity: f})
	}
	if len(keyword) > 0 {
		scoped = scoped.Where("name like ? OR local_address like ? OR client_id like ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	if err := scoped.Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *wireGuardQuery) AdminListWireGuardsWithClientID(clientID string) ([]*models.WireGuard, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var list []*models.WireGuard
	if err := db.Where(&models.WireGuard{WireGuardEntity: &models.WireGuardEntity{ClientID: clientID}}).
		Preload("AdvertisedEndpoints").Preload("Network").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *wireGuardQuery) AdminListWireGuardsWithNetworkIDs(networkIDs []uint) ([]*models.WireGuard, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var list []*models.WireGuard
	if err := db.Where("network_id IN ?", networkIDs).
		Preload("AdvertisedEndpoints").Preload("Network").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (q *wireGuardQuery) CountWireGuardsWithFilters(userInfo models.UserInfo, filter *models.WireGuardEntity, keyword string) (int64, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	var count int64
	base := db.Model(&models.WireGuard{}).Where(&models.WireGuard{WireGuardEntity: &models.WireGuardEntity{
		UserId:   uint32(userInfo.GetUserID()),
		TenantId: uint32(userInfo.GetTenantID()),
	}})
	if filter != nil {
		f := &models.WireGuardEntity{}
		if len(filter.ClientID) > 0 {
			f.ClientID = filter.ClientID
		}
		if filter.NetworkID != 0 {
			f.NetworkID = filter.NetworkID
		}
		base = base.Where(&models.WireGuard{WireGuardEntity: f})
	}
	if len(keyword) > 0 {
		base = base.Where("name like ? OR local_address like ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if err := base.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
