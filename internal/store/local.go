package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"Tide-Sync/internal/config"
)

// localStore 把本地目录当作后端，供测试与本地模式使用。
type localStore struct {
	root string
}

func newLocal(cfg config.Storage) (Backend, error) {
	if cfg.LocalDir == "" {
		return nil, fmt.Errorf("local_dir 未配置")
	}
	abs, err := filepath.Abs(cfg.LocalDir)
	if err != nil {
		return nil, err
	}
	return &localStore{root: abs}, nil
}

// resolve 把对象 key 映射为本地路径，并阻止越出 root 的路径穿越。
func (s *localStore) resolve(key string) (string, error) {
	clean := filepath.Clean("/" + filepath.FromSlash(key))
	p := filepath.Join(s.root, clean)
	if p != s.root && !strings.HasPrefix(p, s.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("key 越出根目录: %q", key)
	}
	return p, nil
}

func (s *localStore) Put(_ context.Context, key, localPath string) error {
	dst, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return copyFile(localPath, dst)
}

func (s *localStore) Get(_ context.Context, key, localPath string) error {
	src, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	return copyFile(src, localPath)
}

func (s *localStore) List(_ context.Context, prefix string) ([]string, error) {
	base, err := s.resolve(prefix)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
