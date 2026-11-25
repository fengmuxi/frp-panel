package middleware

import (
	"fmt"
	"regexp"

	"github.com/gin-gonic/gin"
)

// logger.go 访问日志脱敏（安全审计复查 2026-11-19 P2）：
// WS 场景 JWT 经 ?token= query 参数传递，gin 默认 Logger 会把带 token 的 URI
// 全文写入访问日志——日志持有者可窃取会话。此 formatter 对 token 参数打码。

var tokenQueryPattern = regexp.MustCompile(`(?i)([?&]token=)[^&\s]*`)

// MaskTokenInPath 对 URI 中的 token 参数打码（供日志 formatter 使用）
func MaskTokenInPath(path string) string {
	return tokenQueryPattern.ReplaceAllString(path, "${1}***")
}

// GinAccessLogFormatter gin 访问日志 formatter：格式与 gin 默认一致，
// 仅对 ?token= 打码
func GinAccessLogFormatter() gin.LogFormatter {
	return func(param gin.LogFormatterParams) string {
		path := MaskTokenInPath(param.Path)
		statusColor := param.StatusCodeColor()
		methodColor := param.MethodColor()
		resetColor := param.ResetColor()
		if param.Latency > 0 {
			return fmt.Sprintf("[GIN] %v |%s %3d %s| %13v | %15s |%s %-7s %s %#v\n%s",
				param.TimeStamp.Format("2006/01/02 - 15:04:05"),
				statusColor, param.StatusCode, resetColor,
				param.Latency,
				param.ClientIP,
				methodColor, param.Method, resetColor,
				path,
				param.ErrorMessage)
		}
		// 与上面 latency>0 分支保持同一套动词（原来少一个 %s 参数，
		// 零延迟请求会输出 "%!s(MISSING)" 且 go vet 报错）
		return fmt.Sprintf("[GIN] %v | %3d | %13v | %15s | %-7s %#v\n%s",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			param.StatusCode, param.Latency,
			param.ClientIP,
			param.Method, path, param.ErrorMessage)
	}
}
