package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validCfg = `
[storage]
type = "local"
local_dir = "/tmp/mirror"
prefix = "tide-sync"

[logging]
format = "json"

[[jobs]]
name = "appA"
source = "/opt/backup/appA"
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeTemp(t, validCfg))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Prefix != "tide-sync" {
		t.Fatalf("prefix = %q", cfg.Storage.Prefix)
	}
	if len(cfg.Jobs) != 1 || cfg.Jobs[0].Name != "appA" {
		t.Fatalf("jobs = %+v", cfg.Jobs)
	}
	if cfg.Defaults.Concurrency != DefaultConcurrency {
		t.Fatalf("concurrency 默认值 = %d", cfg.Defaults.Concurrency)
	}
	if cfg.Defaults.StateFile != DefaultStateFile {
		t.Fatalf("state default = %q", cfg.Defaults.StateFile)
	}
}

func TestLoadEnvExpand(t *testing.T) {
	t.Setenv("MY_AK", "abc123")
	cfgText := strings.Replace(validCfg, `local_dir = "/tmp/mirror"`, `local_dir = "${MY_AK}"`, 1)
	cfg, err := Load(writeTemp(t, cfgText))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.LocalDir != "abc123" {
		t.Fatalf("local_dir = %q", cfg.Storage.LocalDir)
	}
}

func TestLoadMissingEnvFails(t *testing.T) {
	cfgText := strings.Replace(validCfg, `local_dir = "/tmp/mirror"`, `local_dir = "${NO_SUCH_VAR_XYZ}"`, 1)
	if _, err := Load(writeTemp(t, cfgText)); err == nil {
		t.Fatal("缺少环境变量时应报错")
	}
}

func TestValidateDuplicateJob(t *testing.T) {
	cfgText := validCfg + "\n[[jobs]]\nname = \"appA\"\nsource = \"/tmp/x\"\n"
	if _, err := Load(writeTemp(t, cfgText)); err == nil {
		t.Fatal("重复 job 名应报错")
	}
}

func TestValidateBadStorageType(t *testing.T) {
	cfgText := strings.Replace(validCfg, `type = "local"`, `type = "ftp"`, 1)
	if _, err := Load(writeTemp(t, cfgText)); err == nil {
		t.Fatal("未知 storage.type 应报错")
	}
}

func TestRedactedHidesSecret(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.AccessKeySecret = "supersecretkey"
	if strings.Contains(cfg.Redacted(), "supersecretkey") {
		t.Fatal("脱敏输出不应包含完整密钥")
	}
}
