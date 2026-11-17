package checker

import "regexp"

// 报告可能被 --out 落盘或粘贴到工单/issue，渲染层对失败消息中的
// 凭证类内容统一脱敏。三类模式：
//  1. URL userinfo：scheme://user:pass@host
//  2. mysql DSN：user:pass@tcp(host:port)/db
//  3. 键值对口令：password=xxx / "secret": "xxx" / token: xxx
var (
	reURLUserInfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^/@\s:]+):([^/@\s]+)@`)
	reMySQLDSN    = regexp.MustCompile(`([a-zA-Z0-9_.%-]+):([^@/\s]+)@tcp\(`)
	// \b 防止误伤包含关键词子串的字段名（如 connect_secret 里的 secret）
	reKVPairs = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key)\b(\s*["']?\s*[:=]\s*"?)([^&\s"']*)`)
)

// Redact 对单行文本做敏感信息脱敏，无凭证时原样返回。
func Redact(s string) string {
	s = reURLUserInfo.ReplaceAllString(s, "${1}***:***@")
	s = reMySQLDSN.ReplaceAllString(s, "${1}:***@tcp(")
	s = reKVPairs.ReplaceAllString(s, "${1}${2}***")
	return s
}

// redactReport 就地脱敏报告中的自由文本字段（message/详情/建议/配置引用）。
// Render 是报告的唯一出口，CLI 一次运行只渲染一次，就地修改是安全的。
func redactReport(report *RunReport) {
	for i := range report.Results {
		r := &report.Results[i]
		r.Message = Redact(r.Message)
		r.FixHint = Redact(r.FixHint)
		for j := range r.Details {
			r.Details[j] = Redact(r.Details[j])
		}
		for j := range r.ConfigRefList {
			r.ConfigRefList[j] = Redact(r.ConfigRefList[j])
		}
	}
}
