package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// cfgWithSources 生成一份最小配置，源目录为给定绝对路径。
func cfgWithSources(paths ...string) string {
	var b strings.Builder
	b.WriteString(`
[storage]
type = "local"
local_dir = "mirror"
prefix = "tide-sync"

[logging]
format = "json"
`)
	for _, p := range paths {
		fmt.Fprintf(&b, "\n[[sources]]\npath = %q\n", filepath.ToSlash(p))
	}
	return b.String()
}

func TestLoadValid(t *testing.T) {
	base := t.TempDir()
	p := writeTemp(t, cfgWithSources(filepath.Join(base, "backup")))
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Sources) != 1 {
		t.Fatalf("sources = %+v", cfg.Sources)
	}
	// 未配置 state_file 时，默认落在“配置文件所在目录”下
	want := filepath.Join(filepath.Dir(p), DefaultStateFileName)
	if cfg.Defaults.StateFile != want {
		t.Fatalf("state 默认 = %q，期望 %q", cfg.Defaults.StateFile, want)
	}
}

// 相对路径必须相对“配置文件所在目录”解析：这是“任意目录可运行”的关键。
func TestRelativePathsResolveToConfigDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	content := fmt.Sprintf(`
[storage]
type = "local"
local_dir = "mirror"
prefix = "tide-sync"

[logging]
format = "json"
file = "logs/tide-sync.log"

[defaults]
state_file = "data/state.json"

[[sources]]
path = %q
`, filepath.ToSlash(filepath.Join(dir, "backup")))
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

func TestSourceRequired(t *testing.T) {
	cfgText := `
[storage]
type = "local"
local_dir = "mirror"
`
	if _, err := Load(writeTemp(t, cfgText)); err == nil {
		t.Fatal("没有 [[sources]] 应报错")
	}
}

func TestSourceMustBeRooted(t *testing.T) {
	if _, err := Load(writeTemp(t, cfgWithSources("relative/dir"))); err == nil {
		t.Fatal("相对路径的源目录应报错")
	}
}

func TestNestedSourcesRejected(t *testing.T) {
	base := t.TempDir()
	outer := filepath.Join(base, "backup")
	inner := filepath.Join(outer, "appA")
	if _, err := Load(writeTemp(t, cfgWithSources(outer, inner))); err == nil {
		t.Fatal("嵌套源目录应报错（会重复上传）")
	}
}

func TestDuplicateSourcesRejected(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, "backup")
	if _, err := Load(writeTemp(t, cfgWithSources(p, p))); err == nil {
		t.Fatal("重复源目录应报错")
	}
}

func TestSiblingSourcesOK(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "backup")
	b := filepath.Join(base, "data")
	if _, err := Load(writeTemp(t, cfgWithSources(a, b))); err != nil {
		t.Fatalf("并列源目录应通过: %v", err)
	}
}

func TestAbsolutePathsKept(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "mirror")
	cfgText := strings.Replace(cfgWithSources(filepath.Join(t.TempDir(), "backup")),
		`local_dir = "mirror"`, fmt.Sprintf("local_dir = %q", filepath.ToSlash(abs)), 1)
	cfg, err := Load(writeTemp(t, cfgText))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.LocalDir != filepath.ToSlash(abs) {
		t.Fatalf("local_dir = %q，期望 %q", cfg.Storage.LocalDir, filepath.ToSlash(abs))
	}
}

func TestLoadEnvExpand(t *testing.T) {
	t.Setenv("MY_DIR", filepath.ToSlash(filepath.Join(t.TempDir(), "x")))
	cfgText := strings.Replace(cfgWithSources(filepath.Join(t.TempDir(), "backup")),
		`local_dir = "mirror"`, `local_dir = "${MY_DIR}"`, 1)
	cfg, err := Load(writeTemp(t, cfgText))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.HasSuffix(cfg.Storage.LocalDir, "x") {
		t.Fatalf("local_dir = %q", cfg.Storage.LocalDir)
	}
}

func TestLoadMissingEnvFails(t *testing.T) {
	cfgText := strings.Replace(cfgWithSources(filepath.Join(t.TempDir(), "backup")),
		`local_dir = "mirror"`, `local_dir = "${NO_SUCH_VAR_XYZ}"`, 1)
	if _, err := Load(writeTemp(t, cfgText)); err == nil {
		t.Fatal("缺少环境变量时应报错")
	}
}

func TestValidateBadStorageType(t *testing.T) {
	cfgText := strings.Replace(cfgWithSources(filepath.Join(t.TempDir(), "backup")),
		`type = "local"`, `type = "ftp"`, 1)
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
