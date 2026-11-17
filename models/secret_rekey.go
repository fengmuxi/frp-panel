package models

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// secret_rekey.go 敏感字段加密密钥轮换（安全审计阶段 4 运维配套）：
// 修改 GLOBAL_SECRET 后，既有 "enc:v1:" 密文无法被新密钥解密（DecryptSecret 返回空串，
// 节点会掉线）。`frpp migrate rekey` 在旧密钥仍可用时把全部密文用旧密钥解出、
// 新密钥重加密回写，实现透明换钥；存量明文一并升级为密文。
//
// 实现要点：
//   - 仅操作 raw 层：行读取走无钩子的私有 struct（Client/Server 的 AfterFind 会按
//     进程密钥解密、失败置空，用带钩子模型读 raw 值会污染数据），写回走原生 SQL，
//     绕过 BeforeSave/AfterFind；
//   - 幂等可重复：已是新密钥可解的密文行跳过；执行中断可重跑；
//   - 单行失败即中止并报出主键，已写回的行已与新钥一致，不存在半新旧混合状态。

// RekeyResult rekey 处理统计。
type RekeyResult struct {
	ClientsRekeyed   int
	ServersRekeyed   int
	SMTPRekeyed      bool
	SkippedPlaintext int // 执行前仍是明文的行数（一并升级为密文）
}

// decryptWithKey 用指定密钥解密 "enc:v1:" 密文；非密文原样返回，解密失败返回错误。
func decryptWithKey(value string, key []byte) (string, error) {
	if value == "" || !strings.HasPrefix(value, secretPrefix) {
		return value, nil
	}
	return openSecretWithKey(value, key)
}

// rekeyClientRow/rekeyServerRow 无钩子的裸行（字段名与列一一对应，避开加解密钩子）。
type rekeyClientRow struct {
	ClientID      string `gorm:"column:client_id"`
	ConnectSecret string `gorm:"column:connect_secret"`
}

type rekeyServerRow struct {
	ServerID      string `gorm:"column:server_id"`
	ConnectSecret string `gorm:"column:connect_secret"`
}

type rekeySMTPRow struct {
	ID       int    `gorm:"column:id"`
	Password string `gorm:"column:password"`
}

// RekeySecretEncryption 把库内敏感密文从 oldSecret 派生密钥轮换到 newSecret 派生密钥。
// 调用方需先停止 master（与 `frpp migrate up` 同一约束）。
func RekeySecretEncryption(db *gorm.DB, oldSecret, newSecret string) (*RekeyResult, error) {
	if strings.TrimSpace(oldSecret) == "" || strings.TrimSpace(newSecret) == "" {
		return nil, fmt.Errorf("old and new secrets are both required")
	}
	if oldSecret == newSecret {
		return nil, fmt.Errorf("new secret equals old secret, nothing to rekey")
	}

	oldKey := DeriveSecretKey(oldSecret)
	newKey := DeriveSecretKey(newSecret)
	res := &RekeyResult{}

	// rekeyRow 处理单行：已是新钥可解的密文行跳过（幂等）；否则用旧钥解出、新钥重写。
	// 不能用"密文相等"判断跳过 —— GCM 随机 nonce 使同明文每次加密产物不同。
	rekeyRow := func(raw string, label string) (string, bool, error) {
		if raw == "" {
			return "", false, nil
		}
		if _, errNew := openSecretWithKey(raw, newKey); errNew == nil {
			return raw, false, nil
		}
		if !strings.HasPrefix(raw, secretPrefix) {
			res.SkippedPlaintext++
		}
		plain, err := decryptWithKey(raw, oldKey)
		if err != nil {
			return "", false, fmt.Errorf("%s: %w", label, err)
		}
		enc, err := sealSecretWithKey(plain, newKey)
		if err != nil {
			return "", false, fmt.Errorf("%s: %w", label, err)
		}
		return enc, true, nil
	}

	var clients []rekeyClientRow
	if err := db.Table("clients").Unscoped().Find(&clients).Error; err != nil {
		return nil, err
	}
	for _, c := range clients {
		enc, changed, err := rekeyRow(c.ConnectSecret, fmt.Sprintf("clients.client_id=%s", c.ClientID))
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		if err := db.Exec("UPDATE clients SET connect_secret = ? WHERE client_id = ?", enc, c.ClientID).Error; err != nil {
			return nil, err
		}
		res.ClientsRekeyed++
	}

	var servers []rekeyServerRow
	if err := db.Table("servers").Unscoped().Find(&servers).Error; err != nil {
		return nil, err
	}
	for _, s := range servers {
		enc, changed, err := rekeyRow(s.ConnectSecret, fmt.Sprintf("servers.server_id=%s", s.ServerID))
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		if err := db.Exec("UPDATE servers SET connect_secret = ? WHERE server_id = ?", enc, s.ServerID).Error; err != nil {
			return nil, err
		}
		res.ServersRekeyed++
	}

	if db.Migrator().HasTable(&SMTPSettings{}) {
		var smtps []rekeySMTPRow
		if err := db.Table("smtp_settings").Unscoped().Find(&smtps).Error; err != nil {
			return nil, err
		}
		for _, sm := range smtps {
			enc, changed, err := rekeyRow(sm.Password, fmt.Sprintf("smtp_settings.id=%d", sm.ID))
			if err != nil {
				return nil, err
			}
			if !changed {
				continue
			}
			if err := db.Exec("UPDATE smtp_settings SET password = ? WHERE id = ?", enc, sm.ID).Error; err != nil {
				return nil, err
			}
			res.SMTPRekeyed = true
		}
	}
	return res, nil
}
