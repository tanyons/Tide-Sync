// Package config 负责读取并校验配置文件。
// 唯一职责：把 TOML 文件 + 环境变量，解析成强类型 Config，并做完整性校验。
// 不做任何其它事（不建 OSS client、不碰文件系统扫描）。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// 默认值（集中在一处，避免散落）。凡“可写活”的常量都在此，可被配置文件覆盖。
const (
	DefaultStateFileName        = "state.json"
	DefaultPrefix               = "tide-sync"
	DefaultConcurrency          = 4
	DefaultRetry                = 3
	DefaultRetryBackoffSeconds  = 1
	DefaultMaxSizeMB            = 50
	DefaultMaxBackups           = 7
	DefaultLogMaxAgeDays        = 0
	DefaultLogCompress          = false
	DefaultDateDirRe            = `^log-\d{4}-\d{2}-\d{2}$`
	DefaultListPageSize         = 1000
	DefaultNotifyTimeoutSeconds = 10
	DefaultExpireDays           = 30
	DefaultAbortDays            = 3
)

// Config 是整份配置的强类型表示。运行期只读这个结构体，不再碰原始 map。
type Config struct {
	Storage   Storage   `koanf:"storage"`
	Logging   Logging   `koanf:"logging"`
	Notify    Notify    `koanf:"notify"`
	Lifecycle Lifecycle `koanf:"lifecycle"`
	Defaults  Defaults  `koanf:"defaults"`
	Sources   []Source  `koanf:"sources"`
}

type Storage struct {
	Type             string `koanf:"type"` // oss | local
	Endpoint         string `koanf:"endpoint"`
	Bucket           string `koanf:"bucket"`
	AccessKeyID      string `koanf:"access_key_id"`
	AccessKeySecret  string `koanf:"access_key_secret"`
	Prefix           string `koanf:"prefix"`
	InternalEndpoint bool   `koanf:"internal_endpoint"`
	LocalDir         string `koanf:"local_dir"`      // type=local 时使用
	ListPageSize     int    `koanf:"list_page_size"` // 列举分页大小
}

type Logging struct {
	Level      string `koanf:"level"`  // debug|info|warn|error
	File       string `koanf:"file"`   // 空=仅 stdout
	Format     string `koanf:"format"` // json|text
	MaxSizeMB  int    `koanf:"max_size_mb"`
	MaxBackups int    `koanf:"max_backups"`
	MaxAgeDays int    `koanf:"max_age_days"` // 0=不按天数清理
	Compress   bool   `koanf:"compress"`     // 是否压缩旧日志
}

type Notify struct {
	TimeoutSeconds int      `koanf:"timeout_seconds"` // 单次 webhook 请求超时
	Feishu         Feishu   `koanf:"feishu"`
	Dingtalk       Dingtalk `koanf:"dingtalk"`
}

type Feishu struct {
	Enabled bool     `koanf:"enabled"`
	Webhook string   `koanf:"webhook"`
	Events  []string `koanf:"events"`
}

type Dingtalk struct {
	Enabled bool     `koanf:"enabled"`
	Webhook string   `koanf:"webhook"`
	Events  []string `koanf:"events"`
}

// Lifecycle 控制 OSS 生命周期规则（由 `tide lifecycle` 使用）。
type Lifecycle struct {
	ExpireDays int `koanf:"expire_days"` // 对象创建满 N 天删除
	AbortDays  int `koanf:"abort_days"`  // 未完成分片 N 天后中止
}

type Defaults struct {
	Host                  string   `koanf:"host"` // 空=os.Hostname()
	StateFile             string   `koanf:"state_file"`
	Exclude               []string `koanf:"exclude"`
	FollowSymlinks        bool     `koanf:"follow_symlinks"`
	Concurrency           int      `koanf:"concurrency"`
	Retry                 int      `koanf:"retry"`
	RetryBackoffSeconds   int      `koanf:"retry_backoff_seconds"`
	SkipCompletedDateDirs bool     `koanf:"skip_completed_datedirs"`
	DateDirRegex          string   `koanf:"date_dir_regex"`
}

// Source 是一个同步源根目录；路径本身即身份（桶内层级按其绝对路径保留）。
type Source struct {
	Path    string   `koanf:"path"`
	Exclude []string `koanf:"exclude"` // 追加，叠加 Defaults.Exclude
}

// Load 读取配置文件：读盘 -> 展开 ${ENV} -> koanf(TOML) -> 补默认值 -> 校验。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	expanded, err := expandEnv(raw)
	if err != nil {
		return nil, err
	}

	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider(expanded), toml.Parser()); err != nil {
		return nil, fmt.Errorf("解析 TOML 失败: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("映射配置失败: %w", err)
	}

	cfg.applyDefaults()
	cfg.resolvePaths(path)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// envRe 只识别 ${NAME} 形式，不碰单独的 $（避免误伤含 $ 的密钥/口令）。
var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandEnv(data []byte) ([]byte, error) {
	var missing []string
	out := envRe.ReplaceAllFunc(data, func(m []byte) []byte {
		name := string(envRe.FindSubmatch(m)[1])
		v, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
			return m
		}
		return []byte(v)
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("环境变量未设置: %s", strings.Join(dedupe(missing), ", "))
	}
	return out, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (c *Config) applyDefaults() {
	if c.Storage.Prefix == "" {
		c.Storage.Prefix = DefaultPrefix
	}
	if c.Storage.ListPageSize <= 0 {
		c.Storage.ListPageSize = DefaultListPageSize
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "json"
	}
	if c.Logging.MaxSizeMB <= 0 {
		c.Logging.MaxSizeMB = DefaultMaxSizeMB
	}
	if c.Logging.MaxBackups <= 0 {
		c.Logging.MaxBackups = DefaultMaxBackups
	}
	if c.Notify.TimeoutSeconds <= 0 {
		c.Notify.TimeoutSeconds = DefaultNotifyTimeoutSeconds
	}
	if c.Lifecycle.ExpireDays <= 0 {
		c.Lifecycle.ExpireDays = DefaultExpireDays
	}
	if c.Lifecycle.AbortDays <= 0 {
		c.Lifecycle.AbortDays = DefaultAbortDays
	}
	if c.Defaults.Concurrency <= 0 {
		c.Defaults.Concurrency = DefaultConcurrency
	}
	if c.Defaults.Retry <= 0 {
		c.Defaults.Retry = DefaultRetry
	}
	if c.Defaults.RetryBackoffSeconds <= 0 {
		c.Defaults.RetryBackoffSeconds = DefaultRetryBackoffSeconds
	}
	if c.Defaults.DateDirRegex == "" {
		c.Defaults.DateDirRegex = DefaultDateDirRe
	}
	if len(c.Defaults.Exclude) == 0 {
		c.Defaults.Exclude = []string{"**/*.tmp", "**/.DS_Store"}
	}
}

// resolvePaths 把配置里的相对路径解析到“配置文件所在目录”，使工具可在任意目录运行。
func (c *Config) resolvePaths(configPath string) {
	base, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		base = filepath.Dir(configPath)
	}
	if c.Defaults.StateFile == "" {
		c.Defaults.StateFile = filepath.Join(base, DefaultStateFileName)
	} else {
		c.Defaults.StateFile = absFrom(base, c.Defaults.StateFile)
	}
	if c.Logging.File != "" {
		c.Logging.File = absFrom(base, c.Logging.File)
	}
	if c.Storage.LocalDir != "" {
		c.Storage.LocalDir = absFrom(base, c.Storage.LocalDir)
	}
}

func absFrom(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// Validate 做完整性校验，任何问题都明确指出。
func (c *Config) Validate() error {
	switch c.Storage.Type {
	case "oss":
		if c.Storage.Endpoint == "" || c.Storage.Bucket == "" ||
			c.Storage.AccessKeyID == "" || c.Storage.AccessKeySecret == "" {
			return fmt.Errorf("storage.type=oss 需要 endpoint/bucket/access_key_id/access_key_secret")
		}
	case "local":
		if c.Storage.LocalDir == "" {
			return fmt.Errorf("storage.type=local 需要 local_dir")
		}
	default:
		return fmt.Errorf("storage.type 无效: %q（可选 oss|local）", c.Storage.Type)
	}

	switch c.Logging.Format {
	case "json", "text":
	default:
		return fmt.Errorf("logging.format 无效: %q（可选 json|text）", c.Logging.Format)
	}

	if _, err := regexp.Compile(c.Defaults.DateDirRegex); err != nil {
		return fmt.Errorf("defaults.date_dir_regex 非法: %w", err)
	}

	if len(c.Sources) == 0 {
		return fmt.Errorf("至少需要一个 [[sources]]")
	}
	for i, s := range c.Sources {
		if s.Path == "" {
			return fmt.Errorf("sources[%d] 缺少 path", i)
		}
		if !isRooted(s.Path) {
			return fmt.Errorf("sources[%d] 的 path 必须是绝对路径: %q", i, s.Path)
		}
	}

	// 源目录不允许重叠/嵌套：否则同一批文件会被上传两份。
	for i := 0; i < len(c.Sources); i++ {
		for j := i + 1; j < len(c.Sources); j++ {
			a := filepath.Clean(c.Sources[i].Path)
			b := filepath.Clean(c.Sources[j].Path)
			if nestedIn(a, b) || nestedIn(b, a) {
				return fmt.Errorf("源目录重叠，会重复上传: %q 与 %q", a, b)
			}
		}
	}
	return nil
}

// isRooted 判断是否“根起”的路径。Windows 上 "/opt/x" 不算绝对路径，
// 但生产目标是 Linux，这里同时接受它以方便跨平台编写/测试配置。
func isRooted(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(filepath.ToSlash(p), "/")
}

// nestedIn 判断 child 是否等于 parent 或在 parent 之下。
func nestedIn(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Host 返回主机标识：优先用配置，否则用 os.Hostname()。
func (c *Config) Host() (string, error) {
	if h := strings.TrimSpace(c.Defaults.Host); h != "" {
		return h, nil
	}
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "", fmt.Errorf("无法获取主机名，请在 defaults.host 中显式指定")
	}
	return h, nil
}

// Redacted 返回脱敏后的配置文本，供 `tide config check` 打印。
func (c *Config) Redacted() string {
	var b strings.Builder
	fmt.Fprintf(&b, "storage.type     = %s\n", c.Storage.Type)
	fmt.Fprintf(&b, "storage.endpoint = %s\n", c.Storage.Endpoint)
	fmt.Fprintf(&b, "storage.bucket   = %s\n", c.Storage.Bucket)
	fmt.Fprintf(&b, "storage.prefix   = %s\n", c.Storage.Prefix)
	fmt.Fprintf(&b, "storage.ak       = %s\n", redact(c.Storage.AccessKeyID))
	fmt.Fprintf(&b, "storage.sk       = %s\n", redact(c.Storage.AccessKeySecret))
	fmt.Fprintf(&b, "logging.level    = %s (%s)\n", c.Logging.Level, c.Logging.Format)
	fmt.Fprintf(&b, "logging.file     = %s\n", c.Logging.File)
	fmt.Fprintf(&b, "notify.timeout   = %ds\n", c.Notify.TimeoutSeconds)
	fmt.Fprintf(&b, "lifecycle        = expire=%dd abort=%dd\n", c.Lifecycle.ExpireDays, c.Lifecycle.AbortDays)
	fmt.Fprintf(&b, "defaults.host    = %s\n", c.Defaults.Host)
	fmt.Fprintf(&b, "defaults.state   = %s\n", c.Defaults.StateFile)
	fmt.Fprintf(&b, "defaults.exclude = %v\n", c.Defaults.Exclude)
	fmt.Fprintf(&b, "notify.feishu    = %v\n", c.Notify.Feishu.Enabled)
	fmt.Fprintf(&b, "notify.dingtalk  = %v\n", c.Notify.Dingtalk.Enabled)
	for _, s := range c.Sources {
		fmt.Fprintf(&b, "source %-24s exclude=%v\n", s.Path, s.Exclude)
	}
	return b.String()
}

func redact(s string) string {
	if s == "" {
		return "(空)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}
