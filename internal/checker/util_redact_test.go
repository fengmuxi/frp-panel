package checker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactURLUserInfo(t *testing.T) {
	assert.Equal(t,
		"connect postgres://***:***@db:5432/app failed",
		Redact("connect postgres://admin:hunter2@db:5432/app failed"))
	// 普通 URL（无 userinfo）不受影响
	assert.Equal(t,
		"http://127.0.0.1:9000/api/v1/log",
		Redact("http://127.0.0.1:9000/api/v1/log"))
}

func TestRedactMySQLDSN(t *testing.T) {
	assert.Equal(t,
		"open root:***@tcp(127.0.0.1:3306)/frp failed",
		Redact("open root:secret-pass@tcp(127.0.0.1:3306)/frp failed"))
}

func TestRedactKVPairs(t *testing.T) {
	assert.Equal(t, "password=***", Redact("password=abc123"))
	assert.Equal(t, `"password": "***"`, Redact(`"password": "abc123"`))
	assert.Equal(t, "invalid client secret: *** provided", Redact("invalid client secret: xyz provided"))
	// 子串字段名不受误伤：connect_secret 不是独立的 secret 键值对
	assert.Equal(t,
		`connect_secret mismatch`,
		Redact(`connect_secret mismatch`))
	// 无凭证的普通消息原样返回
	assert.Equal(t, "3 条孤儿隧道记录", Redact("3 条孤儿隧道记录"))
}

func TestRedactReport(t *testing.T) {
	report := &RunReport{
		Results: []Result{
			{
				Checker: "db_connect", Group: "env", Status: StatusFail,
				Message: "open root:real-pass@tcp(10.0.0.1:3306)/frp failed",
				Details: []string{"dsn: user:topsecret@tcp(host)/db"},
			},
		},
	}
	redactReport(report)
	assert.Equal(t, "open root:***@tcp(10.0.0.1:3306)/frp failed", report.Results[0].Message)
	assert.Equal(t, "dsn: user:***@tcp(host)/db", report.Results[0].Details[0])
}
