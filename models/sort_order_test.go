package models_test

import (
	"testing"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// sortOrderExpr 与 services/dao/query.go 中的 customSortOrderExpr 保持一致
// （dao 内为包私有常量，此处以字面量验证该 SQL 在 SQLite 下的正确性）
const sortOrderExpr = "(sort_index = 0) ASC, sort_index ASC, created_at ASC"

// 验证自定义排序依赖的三件事：
// 1. SortIndex 字段可通过 AutoMigrate 建列；
// 2. 排序规则：未设置（0）排最后、已设置按序号升序、同序号按创建时间升序；
// 3. 交换重排回写使用的 UPDATE 语句（含默认服务器放行分支）可正常执行。
func TestCustomSortOrder(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&models.Client{}, &models.Server{}))

	base := time.Now()

	newClient := func(id string, sortIndex int, createdAt time.Time) {
		assert.NoError(t, db.Create(&models.Client{
			ClientEntity: &models.ClientEntity{
				ClientID:      id,
				TenantID:      1,
				UserID:        1,
				ConnectSecret: "secret",
				SortIndex:     sortIndex,
				CreatedAt:     createdAt,
				UpdatedAt:     createdAt,
			},
		}).Error)
	}

	// 全部未设置序号时保持创建时间顺序（与升级前的默认表现一致）
	newClient("zero-a", 0, base)
	newClient("zero-b", 0, base.Add(time.Second))
	newClient("zero-c", 0, base.Add(2*time.Second))
	// 已设置的按序号升序排在未设置的前面
	newClient("idx-2", 2, base.Add(3*time.Second))
	newClient("idx-1", 1, base.Add(4*time.Second))

	var clients []*models.Client
	assert.NoError(t, db.Where(&models.Client{
		ClientEntity: &models.ClientEntity{UserID: 1, TenantID: 1},
	}).Order(sortOrderExpr).Find(&clients).Error)

	gotIDs := make([]string, 0, len(clients))
	for _, c := range clients {
		gotIDs = append(gotIDs, c.ClientID)
	}
	assert.Equal(t, []string{"idx-1", "idx-2", "zero-a", "zero-b", "zero-c"}, gotIDs)

	// 交换重排回写：普通服务端 + 默认服务器（user_id/tenant_id 为 0）均能命中
	assert.NoError(t, db.Create(&models.Server{
		ServerEntity: &models.ServerEntity{
			ServerID:      "srv-user",
			TenantID:      1,
			UserID:        1,
			ConnectSecret: "secret",
		},
	}).Error)
	assert.NoError(t, db.Create(&models.Server{
		ServerEntity: &models.ServerEntity{
			ServerID:      "default-server",
			ConnectSecret: "secret",
		},
	}).Error)

	for i, serverID := range []string{"default-server", "srv-user"} {
		assert.NoError(t, db.Model(&models.Server{}).
			Where("server_id = ? AND ((user_id = ? AND tenant_id = ?) OR server_id = ?)",
				serverID, 1, 1, "default-server").
			Update("sort_index", i+1).Error)
	}

	var servers []*models.Server
	assert.NoError(t, db.Order(sortOrderExpr).Find(&servers).Error)
	assert.Len(t, servers, 2)
	// default-server 应被回写为 1（OR server_id 分支放行），srv-user 为 2
	assert.Equal(t, "default-server", servers[0].ServerID)
	assert.Equal(t, 1, servers[0].SortIndex)
	assert.Equal(t, "srv-user", servers[1].ServerID)
	assert.Equal(t, 2, servers[1].SortIndex)
}
