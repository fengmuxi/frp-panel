package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/models"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/google/uuid"
)

// 加密方式取值（对应 smtp_settings.encryption）
const (
	EncryptionNone     = "none"     // 不加密（服务器若通告 STARTTLS 则由 net/smtp 自动升级）
	EncryptionStartTLS = "starttls" // STARTTLS 明文连接后升级
	EncryptionSSL      = "ssl"      // 隐式 TLS（通常 465 端口）
)

const sendTimeout = 10 * time.Second

// SendSync 同步发送一封邮件，带超时，返回错误供调用方展示（用于测试邮件）。
func SendSync(ctx context.Context, cfg *models.SMTPSettings, to []string, content MailContent) error {
	if cfg == nil || cfg.Host == "" {
		return fmt.Errorf("smtp 未配置")
	}
	if len(to) == 0 {
		return fmt.Errorf("没有收件人")
	}
	msg := buildMessage(cfg.FromName, cfg.FromAddress, to, content)
	return doSend(ctx, cfg, to, msg)
}

// SendAsync 异步发送（事件类通知统一走这里，失败仅记日志）。
func SendAsync(ctx context.Context, cfg *models.SMTPSettings, to []string, content MailContent) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Logger(ctx).Errorf("send mail panic: %v", r)
			}
		}()
		if err := SendSync(ctx, cfg, to, content); err != nil {
			logger.Logger(ctx).WithError(err).Errorf("send mail failed, to: %v, subject: [%s]", to, content.Subject)
			return
		}
		logger.Logger(ctx).Infof("send mail success, to: %v, subject: [%s]", to, content.Subject)
	}()
}

// sanitizeDisplayName 清洗发件人显示名称：去除首尾空白与 CR/LF（防邮件头注入）。
func sanitizeDisplayName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

// buildMessage 构造 RFC 5322 邮件。
// fromName 为可选的发件人显示名称：非空时使用 RFC 2047 编码生成
// `显示名 <地址>` 形式（支持中文），为空时保持纯地址。
// HTML 非空时使用 multipart/alternative（text/plain 回退 + text/html），否则纯文本。
func buildMessage(fromName, from string, to []string, content MailContent) []byte {
	fromHeader := from
	if name := sanitizeDisplayName(fromName); name != "" {
		fromHeader = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", name), from)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", fromHeader)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", content.Subject))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")

	if content.HTML == "" {
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=utf-8\r\n")
		fmt.Fprintf(&buf, "\r\n%s", content.Text)
		return buf.Bytes()
	}

	// multipart/alternative：纯文本回退 + HTML 美化版本
	boundary := "frpp-" + uuid.New().String()
	fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%q\r\n", boundary)
	fmt.Fprintf(&buf, "\r\n")
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
	fmt.Fprintf(&buf, "%s\r\n", content.Text)
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: text/html; charset=utf-8\r\n")
	fmt.Fprintf(&buf, "Content-Transfer-Encoding: base64\r\n\r\n")
	// base64 必须按 RFC 2045 折行（每行 ≤76 字符）：整行输出会超过
	// RFC 5321 的 998 字符行限制，被 SMTP 服务器以 500 拒收
	fmt.Fprintf(&buf, "%s\r\n", base64Wrapped([]byte(content.HTML)))
	fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	return buf.Bytes()
}

// base64Wrapped 标准 base64 编码并按 76 字符/行折行（CRLF）。
func base64Wrapped(src []byte) string {
	enc := base64.StdEncoding.EncodeToString(src)
	var b strings.Builder
	b.Grow(len(enc) + len(enc)/76*2 + 2)
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		b.WriteString(enc[i:end])
		b.WriteString("\r\n")
	}
	return b.String()
}

// doSend 按配置的加密方式建立连接、认证并投递。
func doSend(ctx context.Context, cfg *models.SMTPSettings, to []string, msg []byte) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	tlsCfg := &tls.Config{ServerName: cfg.Host}

	var (
		conn net.Conn
		err  error
	)

	switch cfg.Encryption {
	case EncryptionSSL:
		// 隐式 TLS：连接即是加密连接
		dialer := &net.Dialer{Timeout: sendTimeout}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	default:
		// none / starttls 均先建立明文连接，starttls 后续升级
		conn, err = net.DialTimeout("tcp", addr, sendTimeout)
	}
	if err != nil {
		if cfg.Encryption == EncryptionSSL {
			// 隐式 TLS 连非 TLS 端口时，服务端返回明文 banner 会导致
			// "tls: first record does not look like a TLS handshake"——
			// 几乎总是端口与加密方式不匹配（587/25 应使用 STARTTLS），在此直接给出纠正建议
			return fmt.Errorf("连接邮件服务器失败: %w（提示：SSL/TLS 为隐式加密，通常搭配 465 端口；587/25 端口应将加密方式改为 STARTTLS）", err)
		}
		return fmt.Errorf("连接邮件服务器失败: %w", err)
	}

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("初始化 SMTP 客户端失败: %w", err)
	}
	defer client.Quit()

	if cfg.Encryption == EncryptionStartTLS {
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS 失败: %w", err)
		}
	}

	// 有用户名才认证；net/smtp 的 PlainAuth 仅在 TLS 或本机连接上发送凭据
	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}

	if err := client.Mail(cfg.FromAddress); err != nil {
		return fmt.Errorf("发件人地址被拒绝: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("收件人地址被拒绝 %s: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("写入邮件正文失败: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		w.Close()
		return fmt.Errorf("写入邮件正文失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("提交邮件失败: %w", err)
	}
	return nil
}
