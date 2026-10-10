// Package restore 把桶内某个子树恢复到本地目录（目录对目录）。
package restore

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"Tide-Sync/internal/store"
)

// Options 描述一次恢复请求。
type Options struct {
	Prefix string // 桶内根前缀
	Host   string
	Path   string // 桶内子树（相对 host），如 opt/backup/appA/log-2024-06-01；空=整个主机
	Filter string // 可选：doublestar 过滤相对路径
	To     string // 目标目录：子树内容写入此处
}

// Run 把 <prefix>/<host>/<path> 下对象的“内容”恢复到 opt.To，返回恢复文件数。
func Run(ctx context.Context, b store.Backend, opt Options) (int, error) {
	subtree := path.Join(opt.Prefix, opt.Host, filepath.ToSlash(opt.Path))
	objects, err := b.List(ctx, subtree)
	if err != nil {
		return 0, err
	}

	prefixWithSlash := subtree + "/"
	count := 0
	for _, key := range objects {
		if !strings.HasPrefix(key, prefixWithSlash) {
			continue
		}
		rel := strings.TrimPrefix(key, prefixWithSlash)
		if opt.Filter != "" {
			if ok, _ := doublestar.Match(opt.Filter, rel); !ok {
				continue
			}
		}

		dest := filepath.Join(opt.To, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return count, err
		}
		if err := b.Get(ctx, key, dest); err != nil {
			return count, fmt.Errorf("下载 %s 失败: %w", key, err)
		}
		count++
	}
	return count, nil
}

// IsRootPath 判断是否文件系统根目录（恢复默认不允许直接写根）。
func IsRootPath(p string) bool {
	c := filepath.Clean(p)
	return filepath.Dir(c) == c
}
