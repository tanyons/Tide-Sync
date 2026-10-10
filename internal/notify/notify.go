// Package notify 定义告警渠道。全项目仅有的两个接口之一：Notifier。
// 已实现飞书；钉钉同接口预留（配置启用即可用）。
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
	Job       string
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

// wants 判断某状态是否需要通知：默认只通知 failure。
func wants(events []string, status string) bool {
	if len(events) == 0 {
		return status == "failure"
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

func formatEvent(ev Event) string {
	status := "成功"
	if ev.Status == "failure" {
		status = "失败"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【Tide-Sync %s】主机=%s 任务=%s\n", status, ev.Host, ev.Job)
	fmt.Fprintf(&b, "扫描=%d 上传=%d 失败=%d 字节=%d 耗时=%s",
		ev.Scanned, ev.Uploaded, ev.Failures, ev.Bytes,
		ev.EndedAt.Sub(ev.StartedAt).Round(time.Second))
	if ev.Err != "" {
		fmt.Fprintf(&b, "\n错误: %s", ev.Err)
	}
	return b.String()
}

// ---- 飞书 ----

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
		"msg_type": "text",
		"content":  map[string]any{"text": formatEvent(ev)},
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

// ---- 钉钉（预留，接口相同） ----

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
		"text":    map[string]any{"content": formatEvent(ev)},
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
