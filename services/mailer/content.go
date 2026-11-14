package mailer

import (
	"fmt"
	"strings"
	"time"
)

// MailContent 一封邮件的标题与双版本正文：Text 供不支持 HTML 的客户端回退，HTML 为美化模板。
type MailContent struct {
	Subject string
	Text    string
	HTML    string
}

// 邮件徽章级别（决定模板配色）
const (
	levelAlert  = "alert"  // 掉线告警（红）
	levelInfo   = "info"   // 通知（蓝）
	levelOnline = "online" // 上线通知（绿）
	levelTest   = "test"   // 测试（灰）
)

// contentField 邮件正文的键值字段行
type contentField struct {
	Key   string
	Value string
}

// contentParams 渲染参数：徽章、标题、副标题、字段行、可选提示块
type contentParams struct {
	Level    string
	Kind     string
	Title    string
	Subtitle string
	Fields   []contentField
	Notice   string
}

// 模板枚举（SMTP 设置页"按模板发送测试邮件"使用）
const (
	MailTemplateTest          = "test"           // 通用连通性测试
	MailTemplateServerOffline = "server_offline" // 服务端掉线提醒
	MailTemplateClientOffline = "client_offline" // 客户端掉线提醒
	MailTemplateProxyOffline  = "proxy_offline"  // 隧道掉线提醒
	MailTemplateLogin         = "login"          // 用户登录通知
	MailTemplateServerOnline  = "server_online"  // 服务端上线提醒
	MailTemplateClientOnline  = "client_online"  // 客户端上线提醒
	MailTemplateProxyOnline   = "proxy_online"   // 隧道上线提醒
)

// NodeOfflineContent 节点（服务端/客户端）掉线提醒。
func NodeOfflineContent(kind, name, clientID string, offlineAt time.Time) MailContent {
	p := contentParams{
		Level:    levelAlert,
		Kind:     "离线告警",
		Title:    fmt.Sprintf("%s「%s」已离线", kind, name),
		Subtitle: "请检查该节点的进程与网络状态，恢复在线后将自动恢复正常转发。",
		Fields: []contentField{
			{"节点类型", kind},
			{"节点名称", name},
			{"节点 ID", clientID},
			{"掉线时间", formatMailTime(offlineAt)},
		},
	}
	return MailContent{
		Subject: fmt.Sprintf("[frp-panel] %s「%s」掉线提醒", kind, name),
		Text:    renderText(p),
		HTML:    renderHTML(p),
	}
}

// TunnelOfflineContent 隧道掉线提醒。
func TunnelOfflineContent(proxyName, serverID string, foundAt time.Time) MailContent {
	p := contentParams{
		Level:    levelAlert,
		Kind:     "离线告警",
		Title:    fmt.Sprintf("隧道「%s」已掉线", proxyName),
		Subtitle: "该隧道在服务端上的代理已停止运行，请检查对应客户端与本地服务。",
		Fields: []contentField{
			{"隧道名称", proxyName},
			{"服务端 ID", serverID},
			{"发现时间", formatMailTime(foundAt)},
		},
	}
	return MailContent{
		Subject: fmt.Sprintf("[frp-panel] 隧道「%s」掉线提醒", proxyName),
		Text:    renderText(p),
		HTML:    renderHTML(p),
	}
}

// NodeOnlineContent 节点（服务端/客户端）上线提醒。
func NodeOnlineContent(kind, name, clientID string, onlineAt time.Time) MailContent {
	p := contentParams{
		Level:    levelOnline,
		Kind:     "上线通知",
		Title:    fmt.Sprintf("%s「%s」已上线", kind, name),
		Subtitle: "该节点已重新连接到面板，恢复正常工作。",
		Fields: []contentField{
			{"节点类型", kind},
			{"节点名称", name},
			{"节点 ID", clientID},
			{"上线时间", formatMailTime(onlineAt)},
		},
	}
	return MailContent{
		Subject: fmt.Sprintf("[frp-panel] %s「%s」上线提醒", kind, name),
		Text:    renderText(p),
		HTML:    renderHTML(p),
	}
}

// TunnelOnlineContent 隧道上线提醒。
func TunnelOnlineContent(proxyName, serverID string, onlineAt time.Time) MailContent {
	p := contentParams{
		Level:    levelOnline,
		Kind:     "上线通知",
		Title:    fmt.Sprintf("隧道「%s」已上线", proxyName),
		Subtitle: "该隧道在服务端上的代理已恢复运行。",
		Fields: []contentField{
			{"隧道名称", proxyName},
			{"服务端 ID", serverID},
			{"恢复时间", formatMailTime(onlineAt)},
		},
	}
	return MailContent{
		Subject: fmt.Sprintf("[frp-panel] 隧道「%s」上线提醒", proxyName),
		Text:    renderText(p),
		HTML:    renderHTML(p),
	}
}

// LoginContent 用户登录通知。
func LoginContent(username, ip string, loginAt time.Time) MailContent {
	p := contentParams{
		Level:    levelInfo,
		Kind:     "登录通知",
		Title:    "你的账号刚刚成功登录",
		Subtitle: "如非本人操作，请立即前往面板修改密码。",
		Fields: []contentField{
			{"用户名", username},
			{"登录 IP", ip},
			{"登录时间", formatMailTime(loginAt)},
		},
		Notice: "如果这不是你本人的操作，请立即修改密码。",
	}
	return MailContent{
		Subject: "[frp-panel] 登录通知",
		Text:    renderText(p),
		HTML:    renderHTML(p),
	}
}

// TestContent 通用连通性测试邮件。
func TestContent() MailContent {
	p := contentParams{
		Level:    levelTest,
		Kind:     "测试邮件",
		Title:    "SMTP 配置正确",
		Subtitle: "如果你收到了这封邮件，说明当前 SMTP 配置可以正常发信。",
		Fields: []contentField{
			{"发送时间", formatMailTime(time.Now())},
		},
	}
	return MailContent{
		Subject: "[frp-panel] 测试邮件",
		Text:    renderText(p),
		HTML:    renderHTML(p),
	}
}

// BuildSampleContent 按 SMTP 设置页的模板枚举渲染一封"样例数据"的邮件，
// 供"按模板发送测试邮件"使用；ok=false 表示未知模板。
func BuildSampleContent(template string) (content MailContent, ok bool) {
	now := time.Now()
	switch template {
	case MailTemplateServerOffline:
		return NodeOfflineContent("服务端", "test-server", "srv-example-001", now), true
	case MailTemplateClientOffline:
		return NodeOfflineContent("客户端", "test-client", "cli-example-001", now), true
	case MailTemplateProxyOffline:
		return TunnelOfflineContent("test-tunnel", "srv-example-001", now), true
	case MailTemplateLogin:
		return LoginContent("admin", "203.0.113.10", now), true
	case MailTemplateServerOnline:
		return NodeOnlineContent("服务端", "test-server", "srv-example-001", now), true
	case MailTemplateClientOnline:
		return NodeOnlineContent("客户端", "test-client", "cli-example-001", now), true
	case MailTemplateProxyOnline:
		return TunnelOnlineContent("test-tunnel", "srv-example-001", now), true
	}
	return MailContent{}, false
}

func formatMailTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// renderText 纯文本版本（不支持 HTML 的客户端回退）。
func renderText(p contentParams) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", p.Title)
	fmt.Fprintf(&b, "\n")
	for _, f := range p.Fields {
		fmt.Fprintf(&b, "%s: %s\n", f.Key, f.Value)
	}
	if p.Notice != "" {
		fmt.Fprintf(&b, "\n%s\n", p.Notice)
	}
	fmt.Fprintf(&b, "\n本邮件由 frp-panel 邮件通知功能自动发送。")
	return b.String()
}

// levelColor 各级别的徽章配色（与面板主色一致的体系）
func levelColor(level string) (bg, text string) {
	switch level {
	case levelAlert:
		return "#fef2f2", "#dc2626"
	case levelInfo:
		return "#eff6ff", "#2563eb"
	case levelOnline:
		return "#f0fdf4", "#16a34a"
	default:
		return "#f3f4f6", "#6b7280"
	}
}

// renderHTML 渲染统一品牌样式的 HTML 邮件。
// 使用 table 布局 + 全内联样式（兼容 Outlook/Gmail 等客户端，不依赖外部 CSS 与图片）。
func renderHTML(p contentParams) string {
	badgeBg, badgeFg := levelColor(p.Level)

	var fieldRows strings.Builder
	for _, f := range p.Fields {
		fmt.Fprintf(&fieldRows, `
			<tr>
				<td style="padding:10px 0;color:#6b7280;font-size:13px;width:96px;vertical-align:top;">%s</td>
				<td style="padding:10px 0;color:#111827;font-size:13px;font-weight:600;word-break:break-all;">%s</td>
			</tr>`, esc(f.Key), esc(f.Value))
	}
	notice := ""
	if p.Notice != "" {
		notice = fmt.Sprintf(`
			<tr><td style="padding:0 32px 24px;">
				<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#fffbeb;border-radius:8px;">
					<tr><td style="padding:12px 16px;color:#92400e;font-size:13px;">%s</td></tr>
				</table>
			</td></tr>`, esc(p.Notice))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="margin:0;padding:0;background:#f4f5f7;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#f4f5f7;padding:24px 0;">
	<tr><td align="center">
		<table role="presentation" width="600" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:12px;overflow:hidden;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif;">
			<tr>
				<td style="background:#111827;padding:18px 32px;">
					<span style="color:#ffffff;font-size:16px;font-weight:700;letter-spacing:0.5px;">FRP-Panel</span>
					<span style="color:#9ca3af;font-size:12px;margin-left:10px;">隧道面板</span>
				</td>
			</tr>
			<tr>
				<td style="padding:28px 32px 0;">
					<span style="display:inline-block;padding:4px 12px;border-radius:999px;background:%[2]s;color:%[3]s;font-size:12px;font-weight:600;">%[1]s</span>
					<h1 style="margin:14px 0 6px;color:#111827;font-size:19px;line-height:1.4;">%[4]s</h1>
					<p style="margin:0;color:#6b7280;font-size:14px;line-height:1.6;">%[5]s</p>
				</td>
			</tr>
			<tr>
				<td style="padding:20px 32px;">
					<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#f9fafb;border-radius:8px;padding:6px 16px;">%[6]s</table>
				</td>
			</tr>%[7]s
			<tr>
				<td style="background:#f9fafb;padding:14px 32px;border-top:1px solid #e5e7eb;">
					<p style="margin:0;color:#9ca3af;font-size:12px;">本邮件由 frp-panel 邮件通知功能自动发送，请勿直接回复。</p>
				</td>
			</tr>
		</table>
	</td></tr>
</table>
</body>
</html>`,
		esc(p.Kind), badgeBg, badgeFg, esc(p.Title), esc(p.Subtitle), fieldRows.String(), notice)
}

// esc HTML 转义（字段值可能包含用户输入的节点名等）
func esc(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&#34;",
		"'", "&#39;",
	)
	return r.Replace(s)
}
