package dao

import (
	"errors"

	"github.com/fengmuxi/frp-panel/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NotifyPrefsQuery interface {
	GetNotifyPrefs(userID int) (*models.UserNotifySettings, error)
}

type NotifyPrefsMutation interface {
	SaveNotifyPrefs(p *models.UserNotifySettings) error
}

type notifyPrefsQuery struct{ *queryImpl }
type notifyPrefsMutation struct{ *mutationImpl }

func newNotifyPrefsQuery(base *queryImpl) NotifyPrefsQuery {
	return &notifyPrefsQuery{base}
}
func newNotifyPrefsMutation(base *mutationImpl) NotifyPrefsMutation {
	return &notifyPrefsMutation{base}
}

// GetNotifyPrefs 读取指定用户的通知开关配置。
// 用户尚未保存过任何配置时，回退取全局 smtp_settings 中的历史开关值作为默认
// （权限拆分前的全局配置，避免升级后配置丢失）；全局也无记录则返回全关闭的零值。
func (q *notifyPrefsQuery) GetNotifyPrefs(userID int) (*models.UserNotifySettings, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	p := &models.UserNotifySettings{}
	err := db.First(p, userID).Error
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 未配置过：以旧的全局开关作为默认值（保留升级前行为）
	global, gerr := NewQuery(q.ctx).GetSMTPSettings()
	if gerr != nil || global == nil {
		return &models.UserNotifySettings{UserID: userID}, nil
	}
	return &models.UserNotifySettings{
		UserID:              userID,
		EnableServerOffline: global.EnableServerOffline,
		EnableClientOffline: global.EnableClientOffline,
		EnableProxyOffline:  global.EnableProxyOffline,
		EnableLoginNotify:   global.EnableLoginNotify,
		EnableServerOnline:  global.EnableServerOnline,
		EnableClientOnline:  global.EnableClientOnline,
		EnableProxyOnline:   global.EnableProxyOnline,
		EnableQuotaExceeded: global.EnableQuotaExceeded,
		EnableScheduleToggle: global.EnableScheduleToggle,
		OfflineDelayServer:  global.OfflineDelayServer,
		OfflineDelayClient:  global.OfflineDelayClient,
		OfflineDelayProxy:   global.OfflineDelayProxy,
	}, nil
}

// SaveNotifyPrefs 保存指定用户的通知开关配置（按 user_id upsert）
func (m *notifyPrefsMutation) SaveNotifyPrefs(p *models.UserNotifySettings) error {
	if p.UserID <= 0 {
		return errors.New("invalid user id")
	}
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		UpdateAll: true,
	}).Create(p).Error
}
