package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"Tide-Sync/internal/config"
)

func TestWants(t *testing.T) {
	// 未配置 events：成功、失败都通知
	if !wants(nil, "failure") || !wants(nil, "success") {
		t.Fatal("缺省应两种状态都通知")
	}
	if wants([]string{"failure"}, "success") {
		t.Fatal("events=[failure] 时 success 不应通知")
	}
	if !wants([]string{"success", "failure"}, "success") {
		t.Fatal("列出 success 时应通知")
	}
	if !wants([]string{"always"}, "success") {
		t.Fatal("always 应通知")
	}
}

func capturePayload(t *testing.T, events []string, ev Event) map[string]any {
	t.Helper()
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer srv.Close()

	ns := New(config.Notify{Feishu: config.Feishu{Enabled: true, Webhook: srv.URL, Events: events}}, "h1")
	if len(ns) != 1 {
		t.Fatalf("应构建 1 个渠道，得到 %d", len(ns))
	}
	if err := ns[0].Send(context.Background(), ev); err != nil {
		t.Fatalf("Send: %v", err)
	}
	return payload
}

func sampleEvent(status string) Event {
	now := time.Now()
	return Event{
		Host: "web-01", Job: "appA", Status: status,
		StartedAt: now.Add(-8 * time.Second), EndedAt: now,
		Scanned: 120, Uploaded: 3, Bytes: 1048576,
	}
}

func TestFeishuSendsInteractiveCard(t *testing.T) {
	card := capturePayload(t, nil, sampleEvent("success"))["card"].(map[string]any)

	if card["header"].(map[string]any)["template"] != "green" {
		t.Fatalf("成功应绿色，得到 %v", card["header"])
	}
	blob, _ := json.Marshal(card)
	if !strings.Contains(string(blob), "web-01") || !strings.Contains(string(blob), "appA") {
		t.Fatalf("卡片应包含主机与任务: %s", blob)
	}
}

func TestFeishuPayloadIsInteractive(t *testing.T) {
	payload := capturePayload(t, nil, sampleEvent("success"))
	if payload["msg_type"] != "interactive" {
		t.Fatalf("msg_type = %v，期望 interactive", payload["msg_type"])
	}
}

func TestFeishuFailureIsRedAndHasError(t *testing.T) {
	ev := sampleEvent("failure")
	ev.Err = "2 个文件上传失败"
	card := capturePayload(t, nil, ev)["card"].(map[string]any)

	if card["header"].(map[string]any)["template"] != "red" {
		t.Fatal("失败应红色")
	}
	blob, _ := json.Marshal(card)
	if !strings.Contains(string(blob), "2 个文件上传失败") {
		t.Fatalf("卡片应包含错误信息: %s", blob)
	}
}

// 缺省情况下成功也要发（成功/失败各一条）。
func TestFeishuSendsSuccessByDefault(t *testing.T) {
	if capturePayload(t, nil, sampleEvent("success")) == nil {
		t.Fatal("缺省时 success 也应发送")
	}
}

// events=[failure] 时成功不发。
func TestFeishuSkipsSuccessWhenOnlyFailure(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	ns := New(config.Notify{Feishu: config.Feishu{
		Enabled: true, Webhook: srv.URL, Events: []string{"failure"},
	}}, "h1")
	if err := ns[0].Send(context.Background(), Event{Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("events=[failure] 时 success 不应发请求")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:            "512 B",
		1024:           "1.0 KiB",
		1048576:        "1.0 MiB",
		3 * 1073741824: "3.0 GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q，期望 %q", in, got, want)
		}
	}
}
