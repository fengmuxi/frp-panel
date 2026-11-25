package models

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// secret_crypto.go 敏感字段的静态加密（安全审计 P2，迁移 003）：
// Client.ConnectSecret / Server.ConnectSecret / SMTPSettings.Password 此前明文入库，
// DB 文件/备份泄露即等于全部节点凭证与 SMTP 凭证泄露。
//
//   - 密钥由 APP_GLOBAL_SECRET 派生（AES-256-GCM），master 启动时经
//     InitSecretEncryption 注入；修改 GLOBAL_SECRET 会使既有密文不可解密，
//     需按文档重新初始化节点接入；
//   - 密文以 "enc:v1:" 前缀标记：无前缀的存量明文在读取时原样透传
//     （迁移 003 负责把它们升级为密文），加密写入天然幂等。

const secretPrefix = "enc:v1:"

var (
	secretKeyOnce sync.Once
	secretAESKey  []byte
	secretKeySet  bool
)

// InitSecretEncryption 设置敏感字段加密密钥（进程内一次）。
func InitSecretEncryption(globalSecret string) {
	secretKeyOnce.Do(func() {
		k := sha256.Sum256([]byte("frp-panel-secret-encryption:" + globalSecret))
		secretAESKey = k[:]
		secretKeySet = true
	})
}

var errSecretKeyNotSet = errors.New("secret encryption key not initialized")

// DeriveSecretKey 由 GLOBAL_SECRET 派生 AES-256 密钥（公开给 rekey/测试等底层工具）。
func DeriveSecretKey(globalSecret string) []byte {
	k := sha256.Sum256([]byte("frp-panel-secret-encryption:" + globalSecret))
	return k[:]
}

// EncryptSecretWithKey 用指定密钥加密明文（密钥轮换等底层工具用）。
func EncryptSecretWithKey(plain string, key []byte) (string, error) {
	return sealSecretWithKey(plain, key)
}

// DecryptSecretWithKey 用指定密钥解密密文；非密文原样透传，解密失败返回错误。
func DecryptSecretWithKey(value string, key []byte) (string, error) {
	return openSecretWithKey(value, key)
}

// sealSecretWithKey 用指定密钥加密明文，产出 "enc:v1:nonce:ct" 密文（密钥轮换等底层工具用）。
func sealSecretWithKey(plain string, key []byte) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plain), nil)
	return secretPrefix + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(sealed), nil
}

// openSecretWithKey 用指定密钥解密 "enc:v1:" 密文；格式不符或解密失败返回错误。
func openSecretWithKey(value string, key []byte) (string, error) {
	parts := strings.SplitN(strings.TrimPrefix(value, secretPrefix), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("malformed secret ciphertext")
	}
	nonce, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	sealed, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

// EncryptSecret 加密敏感字段；幂等：已带密文前缀或空值原样返回。
// 密钥未初始化时返回错误（调用方决定是跳过还是中止）。
func EncryptSecret(plain string) (string, error) {
	if plain == "" || strings.HasPrefix(plain, secretPrefix) {
		return plain, nil
	}
	if !secretKeySet {
		return "", errSecretKeyNotSet
	}
	return sealSecretWithKey(plain, secretAESKey)
}

// DecryptSecret 解密敏感字段；无前缀（存量明文）原样透传；
// 密文解密失败返回空串并需要调用方记录日志。
func DecryptSecret(value string) string {
	plain, err := DecryptSecretVerbose(value)
	if err != nil {
		return ""
	}
	return plain
}

// DecryptSecretVerbose 解密敏感字段并保留错误信息（钩子/运维工具用）：
// 解密失败（GLOBAL_SECRET 不匹配、密文损坏）时调用方应保留原密文——
// 读-改-写路径若把空串回写会永久抹掉凭证。
func DecryptSecretVerbose(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, secretPrefix) {
		return value, nil
	}
	if !secretKeySet {
		// 密钥未初始化的进程（check/客户端等）无法解密，返回空串避免误用
		return "", errSecretKeyNotSet
	}
	plain, err := openSecretWithKey(value, secretAESKey)
	if err != nil {
		return "", err
	}
	return plain, nil
}
