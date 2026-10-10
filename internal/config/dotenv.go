package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// LoadDotEnv 读取 .env 文件并把 KEY=VALUE 写入进程环境（仅当该变量尚未设置时）。
// 文件不存在时静默返回 nil；格式错误（非空、非注释且缺少 '='）时返回错误。
// 用途：把 OSS AK/SK、webhook 等敏感信息放 .env，供配置里的 ${ENV} 展开。
func LoadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 .env 失败: %w", err)
	}

	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return fmt.Errorf("%s:%d 不是 KEY=VALUE 形式: %q", path, i+1, line)
		}
		key := strings.TrimSpace(line[:eq])
		val := unquote(strings.TrimSpace(line[eq+1:]))
		if key == "" {
			return fmt.Errorf("%s:%d KEY 为空", path, i+1)
		}
		// 已存在的真实环境变量优先，不被 .env 覆盖。
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, val); err != nil {
				return err
			}
		}
	}
	return nil
}

// unquote 去掉成对的首尾引号（单/双）。
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
