package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "# comment\n" +
		"TIDE_TEST_A=hello\n" +
		"export TIDE_TEST_B=\"quoted value\"\n" +
		"\n" +
		"TIDE_TEST_C='single'\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"TIDE_TEST_A", "TIDE_TEST_B", "TIDE_TEST_C"} {
		os.Unsetenv(k)
	}
	// 预置 B，验证 .env 不覆盖已存在的真实环境变量
	t.Setenv("TIDE_TEST_B", "real")

	if err := LoadDotEnv(p); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	if got := os.Getenv("TIDE_TEST_A"); got != "hello" {
		t.Fatalf("A = %q", got)
	}
	if got := os.Getenv("TIDE_TEST_B"); got != "real" {
		t.Fatalf("B 不应被 .env 覆盖，得到 %q", got)
	}
	if got := os.Getenv("TIDE_TEST_C"); got != "single" {
		t.Fatalf("C = %q", got)
	}
}

func TestLoadDotEnvMissingFileOK(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Fatalf("文件不存在应静默通过，得到 %v", err)
	}
}

func TestLoadDotEnvMalformed(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte("NOT_A_PAIR\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadDotEnv(p); err == nil {
		t.Fatal("格式错误应报错")
	}
}
