// Package notify 定义告警渠道。全项目仅有的两个接口之一：Notifier。
// 飞书：直连自定义机器人 webhook，发送 interactive 卡片（非官方 SDK）。
// 钉钉：同接口，发送文本（预留）。
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"Tide-Sync/internal/config"
)

// Event 是一次任务执行的结果事件。
type Event struct {
	Host      string
	Source    string
	Status    string // success | failure
	StartedAt time.Time
	EndedAt   time.Time
	Scanned   int64
	Uploaded  int64
	Failures  int64
	Bytes     int64
	Err       string
}

// Notifier 是告警渠道抽象。
type Notifier interface {
	Name() string
	Send(ctx context.Context, ev Event) error
}

// New 按配置构建启用的通知渠道（超时取配置的 notify.timeout_seconds）。
func New(cfg config.Notify, host string) []Notifier {
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = config.DefaultNotifyTimeoutSeconds
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	var out []Notifier
	if cfg.Feishu.Enabled && cfg.Feishu.Webhook != "" {
		out = append(out, &feishu{webhook: cfg.Feishu.Webhook, events: cfg.Feishu.Events, host: host, client: client})
	}
	if cfg.Dingtalk.Enabled && cfg.Dingtalk.Webhook != "" {
		out = append(out, &dingtalk{webhook: cfg.Dingtalk.Webhook, events: cfg.Dingtalk.Events, host: host, client: client})
	}
	return out
}

// wants 判断某状态是否需要通知：缺省（未配置 events）时成功、失败都通知。
func wants(events []string, status string) bool {
	if len(events) == 0 {
		return true
	}
	for _, e := range events {
		if e == "always" || e == status {
			return true
		}
	}
	return false
}

func postJSON(ctx context.Context, client *http.Client, url string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, buf.String())
	}
	return buf.Bytes(), nil
}

// ---- 飞书（interactive 卡片） ----

type feishu struct {
	webhook string
	events  []string
	host    string
	client  *http.Client
}

func (f *feishu) Name() string { return "feishu" }

func (f *feishu) Send(ctx context.Context, ev Event) error {
	if !wants(f.events, ev.Status) {
		return nil
	}
	payload := map[string]any{
		"msg_type": "interactive",
		"card":     feishuCard(ev),
	}
	body, err := postJSON(ctx, f.client, f.webhook, payload)
	if err != nil {
		return err
	}
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("解析飞书响应失败: %w", err)
	}
	if r.Code != 0 {
		return fmt.Errorf("飞书返回错误 code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}

// feishuCard 组装交互卡片：表头按状态着色，字段两列排布，错误单独一段。
func feishuCard(ev Event) map[string]any {
	icon, title, color := "✅", "备份成功", "green"
	if ev.Status == "failure" {
		icon, title, color = "❌", "备份失败", "red"
	}

	// 目录是完整绝对路径，通常较长：单独占一行；其余指标两列排布。
	elements := []any{
		map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": "**目录**\n" + ev.Source},
		},
		map[string]any{
			"tag": "div",
			"fields": []any{
				cardField("主机", ev.Host),
				cardField("耗时", humanDuration(ev.EndedAt.Sub(ev.StartedAt))),
				cardField("上传", fmt.Sprintf("%d 个 · %s", ev.Uploaded, humanBytes(ev.Bytes))),
				cardField("扫描", fmt.Sprintf("%d", ev.Scanned)),
				cardField("失败", fmt.Sprintf("%d", ev.Failures)),
			},
		},
	}

	if ev.Err != "" {
		elements = append(elements,
			map[string]any{"tag": "hr"},
			map[string]any{
				"tag":  "div",
				"text": map[string]any{"tag": "lark_md", "content": "**错误**\n" + ev.Err},
			},
		)
	}

	elements = append(elements, map[string]any{
		"tag": "note",
		"elements": []any{
			map[string]any{"tag": "plain_text", "content": ev.EndedAt.Format("2006-01-02 15:04:05")},
		},
	})

	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": color,
			"title":    map[string]any{"tag": "plain_text", "content": fmt.Sprintf("%s Tide-Sync %s", icon, title)},
		},
		"elements": elements,
	}
}

func cardField(label, value string) map[string]any {
	return map[string]any{
		"is_short": true,
		"text":     map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**%s**\n%s", label, value)},
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func humanDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

// ---- 钉钉（预留，文本） ----

type dingtalk struct {
	webhook string
	events  []string
	host    string
	client  *http.Client
}

func (d *dingtalk) Name() string { return "dingtalk" }

func (d *dingtalk) Send(ctx context.Context, ev Event) error {
	if !wants(d.events, ev.Status) {
		return nil
	}
	payload := map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": plainText(ev)},
	}
	body, err := postJSON(ctx, d.client, d.webhook, payload)
	if err != nil {
		return err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("解析钉钉响应失败: %w", err)
	}
	if r.ErrCode != 0 {
		return fmt.Errorf("钉钉返回错误 errcode=%d errmsg=%s", r.ErrCode, r.ErrMsg)
	}
	return nil
}

func plainText(ev Event) string {
	status := "成功"
	if ev.Status == "failure" {
		status = "失败"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【Tide-Sync %s】主机=%s 源=%s\n", status, ev.Host, ev.Source)
	fmt.Fprintf(&b, "扫描=%d 上传=%d 失败=%d 字节=%d 耗时=%s",
		ev.Scanned, ev.Uploaded, ev.Failures, ev.Bytes, humanDuration(ev.EndedAt.Sub(ev.StartedAt)))
	if ev.Err != "" {
		fmt.Fprintf(&b, "\n错误: %s", ev.Err)
	}
	return b.String()
}
