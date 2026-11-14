package dao

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func i64(v int64) *int64 { return &v }

// TestComputeProxyStatsDailyUpdate 覆盖今日流量补偿的各分支语义。
func TestComputeProxyStatsDailyUpdate(t *testing.T) {
	t.Run("正常推进_按内存增量累加", func(t *testing.T) {
		// DB: today=100, 上次上报=100；本次上报=150 → today 应为 150
		upd := computeProxyStatsDailyUpdate(
			100, 50, 1000, 800,
			i64(100), i64(50),
			150, 60,
			false, true,
		)
		assert.Equal(t, int64(150), upd.TodayIn)
		assert.Equal(t, int64(60), upd.TodayOut)
		assert.Equal(t, int64(1000), upd.HistoryIn)
		assert.Equal(t, int64(800), upd.HistoryOut)
		assert.Equal(t, int64(150), upd.ReportedIn)
	})

	t.Run("frps重启回退_今日累计保持连续", func(t *testing.T) {
		// DB: today=1000（重启前已上报累计），上次上报=1000；
		// frps 重启后内存从 0 重新累计，本次上报=20 → today 应为 1020 而非 20
		upd := computeProxyStatsDailyUpdate(
			1000, 800, 5000, 4000,
			i64(1000), i64(800),
			20, 15,
			false, true,
		)
		assert.Equal(t, int64(1020), upd.TodayIn)
		assert.Equal(t, int64(815), upd.TodayOut)
		// 历史不受影响
		assert.Equal(t, int64(5000), upd.HistoryIn)
	})

	t.Run("firstSync_视同重启衔接", func(t *testing.T) {
		// 节点重启后首次同步（firstSync=true），即使上报值未回退也按重启衔接
		upd := computeProxyStatsDailyUpdate(
			1000, 800, 5000, 4000,
			i64(1000), i64(800),
			1000, 800,
			true, true,
		)
		assert.Equal(t, int64(2000), upd.TodayIn)
		assert.Equal(t, int64(1600), upd.TodayOut)
	})

	t.Run("回退到零_frps重启且暂无流量_今日保持", func(t *testing.T) {
		upd := computeProxyStatsDailyUpdate(
			1000, 800, 5000, 4000,
			i64(1000), i64(800),
			0, 0,
			false, true,
		)
		assert.Equal(t, int64(1000), upd.TodayIn)
		assert.Equal(t, int64(800), upd.TodayOut)
	})

	t.Run("跨天_昨日今日滚入历史_今日从上报值起算", func(t *testing.T) {
		// DB: today=1000（昨天的），history=5000；跨天后 history=6000，today=本次上报 30
		upd := computeProxyStatsDailyUpdate(
			1000, 500, 5000, 4000,
			i64(1000), i64(500),
			30, 10,
			false, false,
		)
		assert.Equal(t, int64(30), upd.TodayIn)
		assert.Equal(t, int64(10), upd.TodayOut)
		assert.Equal(t, int64(6000), upd.HistoryIn)
		assert.Equal(t, int64(4500), upd.HistoryOut)
	})

	t.Run("迁移首拍_无补偿记录_直接采用上报值", func(t *testing.T) {
		// 旧版本数据没有 ReportedToday 记录：旧口径 today 就是上次上报的覆盖值，
		// 本次上报天然连续，取上报值即可（不会双算）
		upd := computeProxyStatsDailyUpdate(
			980, 490, 5000, 4000,
			nil, nil,
			1000, 500,
			false, true,
		)
		assert.Equal(t, int64(1000), upd.TodayIn)
		assert.Equal(t, int64(500), upd.TodayOut)
		assert.Equal(t, int64(1000), upd.ReportedIn)
	})

	t.Run("脏数据负值_按零兜底", func(t *testing.T) {
		// 防御：old.today 被外部写坏为负时（正常分支各处天然非负），
		// 回退分支 old.today + reported 仍可能为负，兜底为 0
		upd := computeProxyStatsDailyUpdate(
			-50, -50, 5000, 4000,
			i64(100), i64(100),
			20, 20,
			false, true,
		)
		assert.Equal(t, int64(0), upd.TodayIn)
		assert.Equal(t, int64(0), upd.TodayOut)
	})
}
