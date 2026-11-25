package models_test

import (
	"fmt"
	"testing"

	"github.com/fengmuxi/frp-panel/conf"
	"github.com/fengmuxi/frp-panel/models"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// 本文件是全链路审计 2026-11-19（docs/audit-remediation-plan-2026-11-19.md P1-D）的
// 回归测试，采用路线 B：DisableForeignKeyConstraintWhenMigrating=true——
// AutoMigrate 不创建任何物理外键。原因：gorm v1.25.11 对 "references 指向非主键
// 唯一列" 的 belongs-to 声明（如 WireGuard.Client references:ClientID），会把
// 物理 FK 约束建到父表（clients 表上出现指向 wireguards/endpoints 的反向约束，
// 方向错乱、插入父表即失败，实测证据见该文档 P1-D 段）。
// 级联删除语义由服务端显式级联清理承担（P1-C）；本测试守护"不意外创建物理外键"。

func newFKTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), conf.DbGormConfig())
	assert.NoError(t, err)

	assert.NoError(t, db.AutoMigrate(
		&models.Client{},
		&models.Worker{},
		&models.Network{},
		&models.WireGuard{},
		&models.Endpoint{},
		&models.WireGuardLink{},
	))
	return db
}

// 迁移后任何相关表都不得携带物理外键约束（含 gorm 自动生成的反向约束）
func TestForeignKeysNotCreated(t *testing.T) {
	db := newFKTestDB(t)

	type fkRow struct {
		ID    int
		Seq   int
		Table string
		From  string
		To    string
	}
	for _, table := range []string{"clients", "workers", "networks", "wireguards", "endpoints", "wireguard_links"} {
		fks := []fkRow{}
		assert.NoError(t, db.Raw(fmt.Sprintf("PRAGMA foreign_key_list(%s)", table)).Scan(&fks).Error,
			"读取 %s 的外键列表失败", table)
		assert.Len(t, fks, 0, "表 %s 不应有物理外键约束", table)
	}
}

// 删除客户端不产生物理级联：关联行（WG/endpoints/links/worker 关联）由服务端
// 显式级联清理（P1-C）负责，本测试锁定"删除入口不做隐式级联"的现状语义
func TestForeignKeysDeleteClientNoImplicitCascade(t *testing.T) {
	db := newFKTestDB(t)

	client := &models.Client{ClientEntity: &models.ClientEntity{ClientID: "c1", UserID: 1, TenantID: 1}}
	assert.NoError(t, db.Create(client).Error)

	network := &models.Network{NetworkEntity: &models.NetworkEntity{Name: "n1"}}
	assert.NoError(t, db.Create(network).Error)

	wg := &models.WireGuard{
		WireGuardEntity: &models.WireGuardEntity{
			ClientID:  client.ClientID,
			NetworkID: network.ID,
			Name:      "wg1",
		},
	}
	assert.NoError(t, db.Create(wg).Error)

	endpoint := &models.Endpoint{
		EndpointEntity: &models.EndpointEntity{
			ClientID:    client.ClientID,
			WireGuardID: wg.ID,
			Host:        "ep.host",
			Port:        51820,
			Type:        "udp",
		},
	}
	assert.NoError(t, db.Create(endpoint).Error)

	link := &models.WireGuardLink{
		WireGuardLinkEntity: &models.WireGuardLinkEntity{
			FromWireGuardID: wg.ID,
			ToWireGuardID:   wg.ID,
			ToEndpointID:    endpoint.ID,
			NetworkID:       network.ID,
		},
	}
	assert.NoError(t, db.Create(link).Error)

	worker := &models.Worker{WorkerEntity: &models.WorkerEntity{ID: "w1", UserId: 1, TenantId: 1}}
	assert.NoError(t, db.Create(worker).Error)
	assert.NoError(t, db.Model(client).Association("Workers").Append(worker))

	assert.NoError(t, db.Unscoped().Delete(client).Error)

	// 关联行保留：显式级联是删除入口（P1-C）的职责
	for _, model := range []interface{}{
		&models.WireGuard{}, &models.Endpoint{}, &models.WireGuardLink{}, &models.Worker{},
	} {
		var cnt int64
		assert.NoError(t, db.Unscoped().Model(model).Count(&cnt).Error)
		assert.Equal(t, int64(1), cnt)
	}
}
