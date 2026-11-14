package dao

import (
	"errors"

	"github.com/VaalaCat/frp-panel/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// smtpSettingsID 单行配置表的固定主键
const smtpSettingsID = 1

type SMTPSettingsQuery interface {
	GetSMTPSettings() (*models.SMTPSettings, error)
}

type SMTPSettingsMutation interface {
	SaveSMTPSettings(s *models.SMTPSettings) error
}

type smtpSettingsQuery struct{ *queryImpl }
type smtpSettingsMutation struct{ *mutationImpl }

func newSMTPSettingsQuery(base *queryImpl) SMTPSettingsQuery {
	return &smtpSettingsQuery{base}
}
func newSMTPSettingsMutation(base *mutationImpl) SMTPSettingsMutation {
	return &smtpSettingsMutation{base}
}

// GetSMTPSettings 读取 SMTP 配置。
// 记录不存在时返回带默认值的零值配置（端口 465、加密方式 none、开关全关），即"未配置 = 全关闭"。
func (q *smtpSettingsQuery) GetSMTPSettings() (*models.SMTPSettings, error) {
	db := q.ctx.GetApp().GetDBManager().GetDefaultDB()
	s := &models.SMTPSettings{}
	err := db.First(s, smtpSettingsID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &models.SMTPSettings{
				ID:         smtpSettingsID,
				Port:       465,
				Encryption: "none",
			}, nil
		}
		return nil, err
	}
	if s.Port == 0 {
		s.Port = 465
	}
	if s.Encryption == "" {
		s.Encryption = "none"
	}
	return s, nil
}

// SaveSMTPSettings 保存 SMTP 配置（upsert 到固定 ID 的单行）
func (m *smtpSettingsMutation) SaveSMTPSettings(s *models.SMTPSettings) error {
	db := m.ctx.GetApp().GetDBManager().GetDefaultDB()
	s.ID = smtpSettingsID
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(s).Error
}
