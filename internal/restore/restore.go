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
	Job    string
	Dir    string // 可选：仅恢复某个顶层目录，如 log-2024-06-11
	Filter string // 可选：doublestar 过滤相对路径
	To     string // 目标目录
}

// Run 从桶内拉取指定 host/job 的对象到本地目录，返回恢复文件数。
func Run(ctx context.Context, b store.Backend, opt Options) (int, error) {
	objPrefix := path.Join(opt.Prefix, opt.Host, opt.Job)
	objects, err := b.List(ctx, objPrefix)
	if err != nil {
		return 0, err
	}

	prefixWithSlash := objPrefix + "/"
	count := 0
	for _, o := range objects {
		if !strings.HasPrefix(o.Key, prefixWithSlash) {
			continue
		}
		rel := strings.TrimPrefix(o.Key, prefixWithSlash)

		if opt.Dir != "" && !strings.HasPrefix(rel, opt.Dir+"/") {
			continue
		}
		if opt.Filter != "" {
			if ok, _ := doublestar.Match(opt.Filter, rel); !ok {
				continue
			}
		}

		dest := filepath.Join(opt.To, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return count, err
		}
		if err := b.Get(ctx, o.Key, dest); err != nil {
			return count, fmt.Errorf("下载 %s 失败: %w", o.Key, err)
		}
		count++
	}
	return count, nil
}
