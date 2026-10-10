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
	p := writeTemp(t, validCfg)
	cfg, err := Load(p)
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
	// 未配置 state_file 时，默认落在“配置文件所在目录”下，而不是写死的 /opt。
	want := filepath.Join(filepath.Dir(p), DefaultStateFileName)
	if cfg.Defaults.StateFile != want {
		t.Fatalf("state 默认 = %q，期望 %q", cfg.Defaults.StateFile, want)
	}
}

// 相对路径必须相对“配置文件所在目录”解析：这是“任意目录可运行”的关键。
func TestRelativePathsResolveToConfigDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	content := `
[storage]
type = "local"
local_dir = "mirror"
prefix = "tide-sync"

[logging]
format = "json"
file = "logs/tide-sync.log"

[defaults]
state_file = "data/state.json"

[[jobs]]
name = "appA"
source = "/opt/backup/appA"
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	checks := map[string]string{
		"state_file":   cfg.Defaults.StateFile,
		"logging.file": cfg.Logging.File,
		"local_dir":    cfg.Storage.LocalDir,
	}
	wants := map[string]string{
		"state_file":   filepath.Join(dir, "data", "state.json"),
		"logging.file": filepath.Join(dir, "logs", "tide-sync.log"),
		"local_dir":    filepath.Join(dir, "mirror"),
	}
	for k, got := range checks {
		if got != wants[k] {
			t.Errorf("%s = %q，期望 %q", k, got, wants[k])
		}
	}
}

// 绝对路径必须保持不变（用平台绝对路径，避免 Windows 下 "/tmp" 被当成相对路径）。
func TestAbsolutePathsKept(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "mirror")
	cfgText := strings.Replace(validCfg, `local_dir = "/tmp/mirror"`,
		`local_dir = "`+filepath.ToSlash(abs)+`"`, 1)
	cfg, err := Load(writeTemp(t, cfgText))
	if err != nil {
		t.Fatal(err)
	}
	// TOML 里写的是斜杠形式，断言也用同一形式
	want := filepath.ToSlash(abs)
	if cfg.Storage.LocalDir != want {
		t.Fatalf("local_dir = %q，期望 %q", cfg.Storage.LocalDir, want)
	}
}

func TestLoadEnvExpand(t *testing.T) {
	t.Setenv("MY_AK", "abc123")
	cfgText := strings.Replace(validCfg, `local_dir = "/tmp/mirror"`, `local_dir = "${MY_AK}"`, 1)
	cfg, err := Load(writeTemp(t, cfgText))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 展开后的相对值同样按配置目录解析
	if !strings.HasSuffix(cfg.Storage.LocalDir, "abc123") {
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
