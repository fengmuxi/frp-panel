package models

import (
	"strings"

	"gorm.io/gorm"
)

// secret_hooks.go 通过 GORM 钩子实现敏感字段的透明加解密（安全审计 P2 / 迁移 003）：
//   - 写入（BeforeSave）：明文 → 密文（幂等：已带前缀跳过）；
//   - 读取（AfterFind）：密文 → 明文；无前缀的存量明文透传（迁移 003 之前的数据）。
//
// 密钥未初始化的进程（frpp check / 客户端等旁路工具）跳过加密、解密得到空串 ——
// 这些上下文不应读写敏感字段；master 启动时经 InitSecretEncryption 注入密钥。

func encryptSecretField(value string) string {
	if value == "" || strings.HasPrefix(value, secretPrefix) {
		return value
	}
	enc, err := EncryptSecret(value)
	if err != nil {
		// 密钥未初始化等场景：保留明文由调用方自行决定（master 上下文密钥必已就绪）
		return value
	}
	return enc
}

func decryptSecretField(value string) string {
	return DecryptSecret(value)
}

// Client 钩子：ConnectSecret（节点 RPC 接入凭证）
func (c *Client) BeforeSave(tx *gorm.DB) error {
	if c.ClientEntity != nil {
		c.ConnectSecret = encryptSecretField(c.ConnectSecret)
	}
	return nil
}

func (c *Client) AfterFind(tx *gorm.DB) error {
	if c.ClientEntity != nil && strings.HasPrefix(c.ConnectSecret, secretPrefix) {
		c.ConnectSecret = decryptSecretField(c.ConnectSecret)
	}
	return nil
}

// Server 钩子：ConnectSecret（frps 接入凭证）
func (s *Server) BeforeSave(tx *gorm.DB) error {
	if s.ServerEntity != nil {
		s.ConnectSecret = encryptSecretField(s.ConnectSecret)
	}
	return nil
}

func (s *Server) AfterFind(tx *gorm.DB) error {
	if s.ServerEntity != nil && strings.HasPrefix(s.ConnectSecret, secretPrefix) {
		s.ConnectSecret = decryptSecretField(s.ConnectSecret)
	}
	return nil
}

// SMTPSettings 钩子：Password（SMTP 凭证）
func (s *SMTPSettings) BeforeSave(tx *gorm.DB) error {
	s.Password = encryptSecretField(s.Password)
	return nil
}

func (s *SMTPSettings) AfterFind(tx *gorm.DB) error {
	if strings.HasPrefix(s.Password, secretPrefix) {
		s.Password = decryptSecretField(s.Password)
	}
	return nil
}
