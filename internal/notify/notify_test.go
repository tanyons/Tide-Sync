package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"Tide-Sync/internal/config"
)

func TestWants(t *testing.T) {
	if !wants(nil, "failure") {
		t.Fatal("默认应通知 failure")
	}
	if wants(nil, "success") {
		t.Fatal("默认不应通知 success")
	}
	if !wants([]string{"always"}, "success") {
		t.Fatal("always 应通知")
	}
	if wants([]string{"failure"}, "success") {
		t.Fatal("未列入的 success 不应通知")
	}
}

func TestFeishuSend(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer srv.Close()

	ns := New(config.Notify{Feishu: config.Feishu{
		Enabled: true, Webhook: srv.URL, Events: []string{"failure"},
	}}, "h1")
	if len(ns) != 1 {
		t.Fatalf("应构建 1 个通知渠道，得到 %d", len(ns))
	}

	err := ns[0].Send(context.Background(), Event{
		Host: "h1", Job: "appA", Status: "failure", Err: "boom",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if payload["msg_type"] != "text" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestFeishuSkipsSuccessByDefault(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	ns := New(config.Notify{Feishu: config.Feishu{Enabled: true, Webhook: srv.URL}}, "h1")
	if err := ns[0].Send(context.Background(), Event{Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("默认情况下 success 不应发送请求")
	}
}
