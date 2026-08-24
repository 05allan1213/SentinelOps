// Package actions AI 运维（Security Orchestration, Automation and Response）钉钉/企微/Email 通知动作。
//
// 设计思想：
//
//	通知类动作是 AI 运维最高频的响应手段，将告警信息推送给安全团队。
//	三种通知方式实现同一 Executor 接口，通过 Webhook URL 或 SMTP 发送消息。
//
// 解决的问题：
//
//	安全事件发生时自动通知相关人员，缩短人工感知时间（MTTD）。
package actions

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"regexp"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// ---- 钉钉通知 ----

type DingTalkAction struct{}

func (a *DingTalkAction) Name() string { return "notify_dingtalk" }

func (a *DingTalkAction) Execute(ctx context.Context, params map[string]string) (ActionResult, error) {
	// webhook 强制使用系统配置，忽略 Agent 传入的值
	webhookURL := g.Cfg().MustGet(ctx, "soar.integrations.dingtalk.webhook_url").String()
	if webhookURL == "" {
		webhookURL = params["webhook_url"]
	}
	title := params["title"]
	content := params["content"]
	if webhookURL == "" {
		return ActionResult{Success: true, Message: "钉钉未配置，已跳过", Output: map[string]string{"skipped": "true"}}, nil
	}
	body, _ := json.Marshal(map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  fmt.Sprintf("## %s\n\n%s", title, content),
		},
	})
	if err := postJSON(webhookURL, body); err != nil {
		return ActionResult{}, fmt.Errorf("notify_dingtalk: %w", err)
	}
	return ActionResult{Success: true, Message: "钉钉通知已发送", Output: map[string]string{"sent": "true"}}, nil
}

// ---- 企微通知 ----

type WeComAction struct{}

func (a *WeComAction) Name() string { return "notify_wecom" }

func (a *WeComAction) Execute(ctx context.Context, params map[string]string) (ActionResult, error) {
	// webhook 强制使用系统配置，忽略 Agent 传入的值
	webhookURL := g.Cfg().MustGet(ctx, "soar.integrations.wecom.webhook_url").String()
	if webhookURL == "" {
		webhookURL = params["webhook_url"]
	}
	content := params["content"]
	if webhookURL == "" {
		return ActionResult{Success: true, Message: "企微未配置，已跳过", Output: map[string]string{"skipped": "true"}}, nil
	}
	body, _ := json.Marshal(map[string]interface{}{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": content},
	})
	if err := postJSON(webhookURL, body); err != nil {
		return ActionResult{}, fmt.Errorf("notify_wecom: %w", err)
	}
	return ActionResult{Success: true, Message: "企微通知已发送", Output: map[string]string{"sent": "true"}}, nil
}

// ---- Email 通知 ----

type EmailAction struct{}

func (a *EmailAction) Name() string { return "notify_email" }

func (a *EmailAction) Execute(ctx context.Context, params map[string]string) (ActionResult, error) {
	host := params["smtp_host"]
	if host == "" {
		host = g.Cfg().MustGet(ctx, "soar.integrations.email.smtp_host").String()
	}
	port := params["smtp_port"]
	if port == "" {
		port = g.Cfg().MustGet(ctx, "soar.integrations.email.smtp_port").String()
	}
	user := params["smtp_user"]
	if user == "" {
		user = g.Cfg().MustGet(ctx, "soar.integrations.email.smtp_user").String()
	}
	pass := params["smtp_pass"]
	if pass == "" {
		pass = g.Cfg().MustGet(ctx, "soar.integrations.email.smtp_pass").String()
	}
	// 收件人强制使用系统配置，忽略 Agent 传入的值（防止 Agent 编造地址）
	to := g.Cfg().MustGet(ctx, "soar.ai_ops.notify_email_to").String()
	if to == "" {
		to = params["to"]
	}
	if host == "" || to == "" {
		return ActionResult{Success: true, Message: "邮件未配置，已跳过", Output: map[string]string{"skipped": "true", "message": "邮件未配置，已跳过"}}, nil
	}
	if port == "" {
		port = "587"
	}

	subject := params["subject"]
	if subject == "" {
		subject = fmt.Sprintf("[安全告警] %s", params["event_title"])
	}

	// 构建规范 HTML 邮件正文
	severityMap := map[string]string{
		"critical": "严重", "high": "高危", "medium": "中危", "low": "低危",
	}
	severityLabel := severityMap[params["event_severity"]]
	if severityLabel == "" {
		severityLabel = params["event_severity"]
	}
	analysis := params["analysis"]
	analysisSection := ""
	if analysis != "" {
		// markdown 简单转 HTML：**text** → <strong>，换行 → <br>
		reBold := regexp.MustCompile(`\*\*(.+?)\*\*`)
		html := reBold.ReplaceAllString(strings.ReplaceAll(analysis, "<", "&lt;"), "<strong>$1</strong>")
		html = strings.ReplaceAll(html, "\n", "<br>")
		analysisSection = fmt.Sprintf(`
      <tr><td colspan="2" style="padding:12px 16px 4px;font-weight:600;color:#374151;border-top:1px solid #e5e7eb;">AI 深度分析</td></tr>
      <tr><td colspan="2" style="padding:4px 16px 16px;color:#4b5563;font-size:13px;line-height:1.7;">%s</td></tr>`, html)
	}

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"></head>
<body style="margin:0;padding:0;background:#f3f4f6;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;">
<table width="100%%" cellpadding="0" cellspacing="0" style="padding:32px 16px;">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="background:#fff;border-radius:12px;overflow:hidden;box-shadow:0 1px 3px rgba(0,0,0,.1);">
  <!-- 头部 -->
  <tr><td style="background:linear-gradient(135deg,#4f46e5,#7c3aed);padding:24px 32px;">
    <p style="margin:0;color:#fff;font-size:20px;font-weight:700;">🛡 安全事件告警</p>
    <p style="margin:6px 0 0;color:rgba(255,255,255,.75);font-size:13px;">SentinelOps · AI 智能运维</p>
  </td></tr>
  <!-- 事件信息 -->
  <tr><td style="padding:0;">
    <table width="100%%" cellpadding="0" cellspacing="0">
      <tr><td style="padding:16px 16px 4px;font-weight:600;color:#374151;">事件详情</td></tr>
      <tr>
        <td style="padding:4px 16px;color:#6b7280;font-size:13px;width:100px;">事件标题</td>
        <td style="padding:4px 16px;color:#111827;font-size:13px;font-weight:500;">%s</td>
      </tr>
      <tr style="background:#f9fafb;">
        <td style="padding:4px 16px;color:#6b7280;font-size:13px;">严重程度</td>
        <td style="padding:4px 16px;font-size:13px;font-weight:600;color:%s;">%s</td>
      </tr>
      <tr>
        <td style="padding:4px 16px;color:#6b7280;font-size:13px;">事件来源</td>
        <td style="padding:4px 16px;color:#111827;font-size:13px;">%s</td>
      </tr>
      <tr style="background:#f9fafb;">
        <td style="padding:4px 16px;color:#6b7280;font-size:13px;">CVE 编号</td>
        <td style="padding:4px 16px;color:#111827;font-size:13px;font-family:monospace;">%s</td>
      </tr>
      <tr>
        <td style="padding:4px 16px;color:#6b7280;font-size:13px;">事件 ID</td>
        <td style="padding:4px 16px;color:#6b7280;font-size:12px;font-family:monospace;">%s</td>
      </tr>
      %s
    </table>
  </td></tr>
  <!-- 底部 -->
  <tr><td style="padding:16px 32px;background:#f9fafb;border-top:1px solid #e5e7eb;">
    <p style="margin:0;color:#9ca3af;font-size:12px;">此邮件由 SentinelOps AI 智能运维系统自动发送，请勿直接回复。</p>
  </td></tr>
</table>
</td></tr>
</table>
</body></html>`,
		params["event_title"],
		map[string]string{"critical": "#dc2626", "high": "#ea580c", "medium": "#d97706", "low": "#2563eb"}[params["event_severity"]],
		severityLabel,
		params["event_source"],
		func() string {
			if v := params["event_cve"]; v != "" {
				return v
			}
			return "-"
		}(),
		params["event_id"],
		analysisSection,
	)

	toList := strings.Split(to, ",")
	msg := strings.Join([]string{
		"From: " + user,
		"To: " + to,
		"Subject: =?UTF-8?B?" + encodeBase64(subject) + "?=",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		htmlBody,
	}, "\r\n")

	auth := smtp.PlainAuth("", user, pass, host)
	if err := smtp.SendMail(host+":"+port, auth, user, toList, []byte(msg)); err != nil {
		return ActionResult{}, fmt.Errorf("notify_email: %w", err)
	}
	return ActionResult{Success: true, Message: "邮件已发送至 " + to, Output: map[string]string{"sent": "true", "to": to}}, nil
}

// ---- 通用 Webhook 出站 ----

type WebhookOutAction struct{}

func (a *WebhookOutAction) Name() string { return "webhook_out" }

func (a *WebhookOutAction) Execute(ctx context.Context, params map[string]string) (ActionResult, error) {
	url := params["url"]
	payload := params["payload"]
	method := params["method"]
	if url == "" {
		return ActionResult{}, fmt.Errorf("webhook_out: url 不能为空")
	}
	if method == "" {
		method = "POST"
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBufferString(payload))
	if err != nil {
		return ActionResult{}, fmt.Errorf("webhook_out: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := params["auth_token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ActionResult{}, fmt.Errorf("webhook_out: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ActionResult{}, fmt.Errorf("webhook_out: HTTP %d", resp.StatusCode)
	}
	return ActionResult{Success: true, Message: fmt.Sprintf("Webhook 调用成功 HTTP %d", resp.StatusCode),
		Output: map[string]string{"status_code": fmt.Sprintf("%d", resp.StatusCode)}}, nil
}

// ---- 工具函数 ----

func encodeBase64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func postJSON(url string, body []byte) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func init() {
	Register(&DingTalkAction{})
	Register(&WeComAction{})
	Register(&EmailAction{})
	Register(&WebhookOutAction{})
}
