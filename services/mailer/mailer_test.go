package mailer

import (
	"bytes"
	"strings"
	"testing"
)

// TestBuildMessageLineLimit 回归：RFC 5321 要求文本行 ≤998 字节。
// 曾因 HTML base64 整行输出导致服务器以 500 "Line too long" 拒收。
func TestBuildMessageLineLimit(t *testing.T) {
	// 构造超长 HTML（模拟完整模板体，含中文多字节字符）
	longHTML := strings.Repeat("<td style=\"padding:10px;color:#111827;\">隧道流量统计内容</td>", 300)
	content := MailContent{
		Subject: "[frp-panel] 测试邮件",
		Text:    "纯文本回退",
		HTML:    longHTML,
	}
	msg := buildMessage("FRP-Panel", "noreply@example.com", []string{"to@example.com"}, content)

	maxLine := 0
	for _, line := range bytes.Split(msg, []byte("\r\n")) {
		if len(line) > maxLine {
			maxLine = len(line)
		}
	}
	if maxLine > 998 {
		t.Fatalf("line too long: max %d bytes (limit 998)", maxLine)
	}

	// 双版本都在：multipart 头与 base64 折行
	if !bytes.Contains(msg, []byte("multipart/alternative")) {
		t.Fatal("expected multipart/alternative")
	}
	if !bytes.Contains(msg, []byte("Content-Transfer-Encoding: base64")) {
		t.Fatal("expected base64 part")
	}

	// 纯文本路径（HTML 为空）不使用 multipart
	plain := buildMessage("", "noreply@example.com", []string{"to@example.com"},
		MailContent{Subject: "s", Text: "t", HTML: ""})
	if bytes.Contains(plain, []byte("multipart/alternative")) {
		t.Fatal("plain content should not be multipart")
	}
	if !bytes.Contains(plain, []byte("Content-Type: text/plain")) {
		t.Fatal("plain content should declare text/plain")
	}
}
