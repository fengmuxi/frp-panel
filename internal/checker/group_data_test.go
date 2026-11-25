package checker

import (
	"testing"
	"time"

	"github.com/fengmuxi/frp-panel/defs"
	"github.com/stretchr/testify/assert"
)

// SameSite 断言：缺失提示补齐；None 必须配 Secure；Lax/Strict 放行。
func TestCheckCookieAttrsSameSite(t *testing.T) {
	env := &Env{}
	env.Cfg.App.CookieName = "frp-panel-cookie"
	env.Cfg.App.CookieHTTPOnly = true
	env.Cfg.App.CookieAge = 86400
	env.Cfg.App.CookiePath = "/"

	withAttrs := func(attrs string) []string {
		raw := "frp-panel-cookie=token; Path=/; Max-Age=86400; HttpOnly" + attrs
		return checkCookieAttrs([]string{raw}, env)
	}

	assert.Empty(t, withAttrs("; SameSite=Lax"))
	assert.Empty(t, withAttrs("; SameSite=Strict"))
	env.Cfg.App.CookieSecure = true // SameSite=None 必须配合 Secure=true
	assert.Empty(t, withAttrs("; SameSite=None; Secure"))
	env.Cfg.App.CookieSecure = false

	problems := withAttrs("") // 旧版本 master：无 SameSite
	assert.NotEmpty(t, problems)
	assert.Contains(t, problems[0], "缺少 SameSite")

	problems = withAttrs("; SameSite=None") // None 不配 Secure
	assert.NotEmpty(t, problems)
	assert.Contains(t, problems[0], "SameSite=None")
}

// quotaWindowStart 与 biz/master/proxy.QuotaWindowStart 的口径一致：
// 日=当日零点、周=本周一零点、月=本月 1 日零点。
func TestQuotaWindowStart(t *testing.T) {
	// 2026-11-15 是周日：周窗口应回退到 2026-11-09（周一）
	now := time.Date(2026, 11, 15, 15, 4, 5, 0, time.Local)
	assert.Equal(t, time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local), quotaWindowStart(now, defs.QuotaTypeDay))
	assert.Equal(t, time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local), quotaWindowStart(now, defs.QuotaTypeWeek))
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local), quotaWindowStart(now, defs.QuotaTypeMonth))

	// 周一当天应落在自身零点
	monday := time.Date(2026, 11, 9, 10, 0, 0, 0, time.Local)
	assert.Equal(t, time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local), quotaWindowStart(monday, defs.QuotaTypeWeek))
}

func TestHumanBytes(t *testing.T) {
	assert.Equal(t, "0B", humanBytes(0))
	assert.Equal(t, "512B", humanBytes(512))
	assert.Equal(t, "1.0KB", humanBytes(1<<10))
	assert.Equal(t, "1.5MB", humanBytes(int64(1.5*(1<<20))))
	assert.Equal(t, "2.00GB", humanBytes(2<<30))
}
