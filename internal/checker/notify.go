package checker

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fengmuxi/frp-panel/models"
	"github.com/fengmuxi/frp-panel/services/mailer"
)

// notify.go `--notify-mail` 邮件告警：
// 复用面板 SMTP 设置（smtp_settings 表，只读）与统一邮件模板发送检查摘要。
// 相同失败内容 24 小时内不重复发送（状态存 historyDir/notify-state.json，
// 与报告历史共用目录——checker 保持只读原则，不写库）。

// errSMTPNotConfigured SMTP 配置缺失时返回，供调用方输出指引。
var errSMTPNotConfigured = errors.New("SMTP 未配置：请先在面板设置页保存 SMTP 配置")

// notifyState 通知去重状态文件内容。
type notifyState struct {
	Hash   string    `json:"hash"`
	SentAt time.Time `json:"sent_at"`
}

// NotifyMail 把报告中 fail/warn 项经已配置的 SMTP 发送给收件人。
// 返回 (是否实际发送, 错误)；sent=false 且 err=nil 表示内容与上次相同被 24h 去重跳过。
func NotifyMail(ctx context.Context, env *Env, report *RunReport, recipients []string, historyDir string) (bool, error) {
	if len(recipients) == 0 {
		return false, errors.New("收件人为空")
	}
	db, err := env.DB(ctx)
	if err != nil {
		return false, err
	}
	// 先判存在性再查询，避免空表时 gorm 打出 record not found 错误日志
	if !db.Migrator().HasTable(&models.SMTPSettings{}) {
		return false, errSMTPNotConfigured
	}
	smtp := &models.SMTPSettings{}
	var cnt int64
	if err := db.Model(&models.SMTPSettings{}).Count(&cnt).Error; err != nil || cnt == 0 {
		return false, errSMTPNotConfigured
	}
	if err := db.First(smtp, 1).Error; err != nil {
		return false, errSMTPNotConfigured
	}
	if smtp.Host == "" || smtp.FromAddress == "" {
		return false, errSMTPNotConfigured
	}

	// 摘要：fail 全量，warn 截断；内容哈希按检查项+状态集合计算（不含消息文本，
	// 避免消息里的时间戳/数值抖动把 24h 去重打穿）
	var items []mailer.Field
	var failCount, warnCount int
	var hashLines []string
	for _, r := range report.Results {
		switch r.Status {
		case StatusFail:
			failCount++
			items = append(items, mailer.Field{Key: r.Group + "/" + r.Checker, Value: fmt.Sprintf("失败: %s", r.Message)})
			hashLines = append(hashLines, fmt.Sprintf("%s/%s=fail", r.Group, r.Checker))
		case StatusWarn:
			warnCount++
			hashLines = append(hashLines, fmt.Sprintf("%s/%s=warn", r.Group, r.Checker))
		}
	}
	if failCount == 0 && warnCount == 0 {
		items = append(items, mailer.Field{Key: "结论", Value: "全部通过，无失败与告警项"})
	}

	if warnCount > 0 {
		items = append(items, mailer.Field{Key: "告警", Value: fmt.Sprintf("%d 项 warn（明细见本地报告）", warnCount)})
	}
	items = append(items,
		mailer.Field{Key: "主机", Value: report.Host},
		mailer.Field{Key: "版本", Value: report.Version},
		mailer.Field{Key: "检查时间", Value: report.StartedAt.Format("2006-01-02 15:04:05")},
	)

	sort.Strings(hashLines)
	sum := sha1.Sum([]byte(strings.Join(hashLines, "\n")))
	hash := hex.EncodeToString(sum[:])

	// 24h 去重：相同检查项集合不重复打扰
	if historyDir != "" {
		if st, err := readNotifyState(historyDir); err == nil &&
			st.Hash == hash && time.Since(st.SentAt) < 24*time.Hour {
			return false, nil
		}
	}

	title := fmt.Sprintf("健康检查发现 %d 项失败、%d 项告警", failCount, warnCount)
	if len(items) <= 1 && failCount == 0 && warnCount == 0 {
		title = "健康检查全部通过"
	}
	content := mailer.CustomContent("alert", "检查告警", title,
		"由 frpp check --notify-mail 自动发送；明细可运行 frpp check 查看。", "", items)
	if err := mailer.SendSync(ctx, smtp, recipients, content); err != nil {
		return false, fmt.Errorf("send notify mail: %w", err)
	}

	if historyDir != "" {
		if err := writeNotifyState(historyDir, &notifyState{Hash: hash, SentAt: time.Now()}); err != nil {
			// 发送已成功，状态写失败仅影响去重，不作为错误返回
			return true, nil
		}
	}
	return true, nil
}

func notifyStatePath(historyDir string) string {
	return filepath.Join(historyDir, "notify-state.json")
}

func readNotifyState(historyDir string) (*notifyState, error) {
	raw, err := os.ReadFile(notifyStatePath(historyDir))
	if err != nil {
		return nil, err
	}
	st := &notifyState{}
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, err
	}
	return st, nil
}

func writeNotifyState(historyDir string, st *notifyState) error {
	if err := os.MkdirAll(historyDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(notifyStatePath(historyDir), raw, 0o600)
}
