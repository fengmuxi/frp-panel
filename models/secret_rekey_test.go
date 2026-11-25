package models_test

import (
	"strings"
	"testing"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// rekey 前重置进程密钥为指定值（InitSecretEncryption 是 sync.Once，测试内用
// 钩子路径替代：直接构造密文行 + 依赖 rekey 的显式密钥参数，不依赖进程态）。
// 注意：本文件所有用例只走 RekeySecretEncryption 的显式密钥参数，与进程密钥无关。

func seedRekeyDB(t *testing.T) *gorm.DB {
	db := newTestDB(t)
	runDBInit(db)
	return db
}

// 正常轮换：旧钥密文 → 新钥密文，明文经 models 层读回原值。
func TestRekeyRotatesCiphertext(t *testing.T) {
	db := seedRekeyDB(t)

	oldKey := "old-global-secret-0123456789"
	newKey := "new-global-secret-abcdef123456"

	// 直接以旧钥产出密文写入（模拟旧钥时代的库存数据）
	encC, err := models.EncryptSecretWithKey("client-secret-plain", models.DeriveSecretKey(oldKey))
	assert.NoError(t, err)
	encS, err := models.EncryptSecretWithKey("server-secret-plain", models.DeriveSecretKey(oldKey))
	assert.NoError(t, err)
	assert.NoError(t, db.Create(&models.Client{
		ClientEntity: &models.ClientEntity{ClientID: "c1", UserID: 1, TenantID: 1, ConnectSecret: encC},
	}).Error)
	assert.NoError(t, db.Create(&models.Server{
		ServerEntity: &models.ServerEntity{ServerID: "s1", UserID: 1, TenantID: 1, ConnectSecret: encS},
	}).Error)

	res, err := models.RekeySecretEncryption(db, oldKey, newKey)
	assert.NoError(t, err)
	assert.Equal(t, 1, res.ClientsRekeyed)
	assert.Equal(t, 1, res.ServersRekeyed)

	// 库内为新钥密文
	var rawC, rawS string
	assert.NoError(t, db.Raw(`SELECT connect_secret FROM clients WHERE client_id = 'c1'`).Row().Scan(&rawC))
	assert.NoError(t, db.Raw(`SELECT connect_secret FROM servers WHERE server_id = 's1'`).Row().Scan(&rawS))
	assert.True(t, strings.HasPrefix(rawC, "enc:v1:"))
	assert.True(t, strings.HasPrefix(rawS, "enc:v1:"))
	// 旧钥已解不开
	_, errOld := models.DecryptSecretWithKey(rawC, models.DeriveSecretKey(oldKey))
	assert.Error(t, errOld)
	// 新钥解出原值
	plainC, err := models.DecryptSecretWithKey(rawC, models.DeriveSecretKey(newKey))
	assert.NoError(t, err)
	assert.Equal(t, "client-secret-plain", plainC)
	plainS, err := models.DecryptSecretWithKey(rawS, models.DeriveSecretKey(newKey))
	assert.NoError(t, err)
	assert.Equal(t, "server-secret-plain", plainS)
}

// 幂等：重复 rekey 跳过已是新钥的行，不再变更。
func TestRekeyIdempotent(t *testing.T) {
	db := seedRekeyDB(t)
	oldKey := "old-global-secret-0123456789"
	newKey := "new-global-secret-abcdef123456"

	enc, err := models.EncryptSecretWithKey("secret-x", models.DeriveSecretKey(oldKey))
	assert.NoError(t, err)
	assert.NoError(t, db.Create(&models.Client{
		ClientEntity: &models.ClientEntity{ClientID: "c1", UserID: 1, TenantID: 1, ConnectSecret: enc},
	}).Error)

	_, err = models.RekeySecretEncryption(db, oldKey, newKey)
	assert.NoError(t, err)
	var raw1 string
	assert.NoError(t, db.Raw(`SELECT connect_secret FROM clients WHERE client_id = 'c1'`).Row().Scan(&raw1))

	res, err := models.RekeySecretEncryption(db, oldKey, newKey)
	assert.NoError(t, err)
	assert.Equal(t, 0, res.ClientsRekeyed)
	var raw2 string
	assert.NoError(t, db.Raw(`SELECT connect_secret FROM clients WHERE client_id = 'c1'`).Row().Scan(&raw2))
	assert.Equal(t, raw1, raw2)
}

// 存量明文一并升级为新钥密文。
func TestRekeyUpgradesPlaintext(t *testing.T) {
	db := seedRekeyDB(t)
	newKey := "new-global-secret-abcdef123456"

	// 原生 SQL 插入明文行（模拟迁移 003 之前的存量数据；走 GORM Create 会被
	// BeforeSave 钩子先用进程密钥加密）
	assert.NoError(t, db.Exec(
		`INSERT INTO clients (client_id, user_id, tenant_id, connect_secret) VALUES ('c-plain', 1, 1, 'raw-plain')`,
	).Error)

	res, err := models.RekeySecretEncryption(db, "some-old-key-at-least-16-chars", newKey)
	assert.NoError(t, err)
	assert.Equal(t, 1, res.ClientsRekeyed)
	assert.Equal(t, 1, res.SkippedPlaintext)

	var raw string
	assert.NoError(t, db.Raw(`SELECT connect_secret FROM clients WHERE client_id = 'c-plain'`).Row().Scan(&raw))
	plain, err := models.DecryptSecretWithKey(raw, models.DeriveSecretKey(newKey))
	assert.NoError(t, err)
	assert.Equal(t, "raw-plain", plain)
}

// SMTP 单行表密码轮换。
func TestRekeySMTP(t *testing.T) {
	db := seedRekeyDB(t)
	oldKey := "old-global-secret-0123456789"
	newKey := "new-global-secret-abcdef123456"

	enc, err := models.EncryptSecretWithKey("smtp-pass", models.DeriveSecretKey(oldKey))
	assert.NoError(t, err)
	assert.NoError(t, db.Create(&models.SMTPSettings{ID: 1, Host: "smtp.x.com", Password: enc}).Error)

	res, err := models.RekeySecretEncryption(db, oldKey, newKey)
	assert.NoError(t, err)
	assert.True(t, res.SMTPRekeyed)

	var raw string
	assert.NoError(t, db.Raw(`SELECT password FROM smtp_settings WHERE id = 1`).Row().Scan(&raw))
	plain, err := models.DecryptSecretWithKey(raw, models.DeriveSecretKey(newKey))
	assert.NoError(t, err)
	assert.Equal(t, "smtp-pass", plain)
}

// 旧钥不对（换钥后又忘了旧值/填错）时报错并指出主键，不写坏数据。
func TestRekeyWrongOldKeyFails(t *testing.T) {
	db := seedRekeyDB(t)
	oldKey := "old-global-secret-0123456789"

	enc, err := models.EncryptSecretWithKey("secret-y", models.DeriveSecretKey(oldKey))
	assert.NoError(t, err)
	assert.NoError(t, db.Create(&models.Client{
		ClientEntity: &models.ClientEntity{ClientID: "c-bad", UserID: 1, TenantID: 1, ConnectSecret: enc},
	}).Error)

	_, err = models.RekeySecretEncryption(db, "wrong-old-key-1234567890", "new-global-secret-abcdef123456")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "c-bad")

	// 库内仍为旧钥密文，可用旧钥解出
	var raw string
	assert.NoError(t, db.Raw(`SELECT connect_secret FROM clients WHERE client_id = 'c-bad'`).Row().Scan(&raw))
	plain, err := models.DecryptSecretWithKey(raw, models.DeriveSecretKey(oldKey))
	assert.NoError(t, err)
	assert.Equal(t, "secret-y", plain)
}

// 参数防御：旧新相同 / 空值拒绝。
func TestRekeyParamGuard(t *testing.T) {
	db := seedRekeyDB(t)
	_, err := models.RekeySecretEncryption(db, "same-key-1234567890", "same-key-1234567890")
	assert.Error(t, err)
	_, err = models.RekeySecretEncryption(db, "", "new-key-1234567890")
	assert.Error(t, err)
	_, err = models.RekeySecretEncryption(db, "old-key-1234567890", "  ")
	assert.Error(t, err)
}
