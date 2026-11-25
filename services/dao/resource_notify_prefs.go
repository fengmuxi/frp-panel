package dao

import (
	"errors"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/app"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResourceNotifyPrefsQuery interface {
	GetResourceNotifyPrefs(resType, resID string) (*models.ResourceNotifyPrefs, error)
}

type ResourceNotifyPrefsMutation interface {
	SaveResourceNotifyPrefs(p *models.ResourceNotifyPrefs) error
	DeleteResourceNotifyPrefs(resType, resID string) error
}

type resourceNotifyPrefsQuery struct{ *queryImpl }
type resourceNotifyPrefsMutation struct{ *mutationImpl }

func newResourceNotifyPrefsQuery(base *queryImpl) ResourceNotifyPrefsQuery {
	return &resourceNotifyPrefsQuery{base}
}
func newResourceNotifyPrefsMutation(base *mutationImpl) ResourceNotifyPrefsMutation {
	return &resourceNotifyPrefsMutation{base}
}

// deleteResourceNotifyPrefs 包内辅助：供 client/server/proxy 的删除实现级联清理
// 各表 mutation 结构体互相独立，无法直接调用对方的方法，统一经此入口
func deleteResourceNotifyPrefs(ctx *app.Context, resType, resID string) error {
	return (&resourceNotifyPrefsMutation{&mutationImpl{ctx: ctx}}).DeleteResourceNotifyPrefs(resType, resID)
}

// GetResourceNotifyPrefs 读取资源级通知覆盖配置。
// 无记录时返回 Mode=inherit 的零值（跟随用户全局），不报错 —— 这是默认语义。
func (q *resourceNotifyPrefsQuery) GetResourceNotifyPrefs(resType, resID string) (*models.ResourceNotifyPrefs, error) {
	if resType == "" || resID == "" {
		return &models.ResourceNotifyPrefs{Mode: models.NotifyModeInherit}, nil
	}
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	p := &models.ResourceNotifyPrefs{}
	err := db.Where(&models.ResourceNotifyPrefs{
		ResourceType: resType,
		ResourceID:   resID,
	}).First(p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &models.ResourceNotifyPrefs{
				ResourceType: resType,
				ResourceID:   resID,
				Mode:         models.NotifyModeInherit,
			}, nil
		}
		return nil, err
	}
	return p, nil
}

// SaveResourceNotifyPrefs 保存资源级通知配置（按 (resource_type, resource_id) upsert）
func (m *resourceNotifyPrefsMutation) SaveResourceNotifyPrefs(p *models.ResourceNotifyPrefs) error {
	if p.ResourceType == "" || p.ResourceID == "" {
		return errors.New("invalid resource identity")
	}
	if p.Mode == "" {
		p.Mode = models.NotifyModeInherit
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "resource_type"}, {Name: "resource_id"}},
		UpdateAll: true,
	}).Create(p).Error
}

// DeleteResourceNotifyPrefs 删除资源的通知覆盖配置（资源删除时级联清理，避免孤儿记录）
func (m *resourceNotifyPrefsMutation) DeleteResourceNotifyPrefs(resType, resID string) error {
	if resType == "" || resID == "" {
		return nil
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Where(&models.ResourceNotifyPrefs{
		ResourceType: resType,
		ResourceID:   resID,
	}).Delete(&models.ResourceNotifyPrefs{}).Error
}
