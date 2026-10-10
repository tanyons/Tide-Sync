// Package store 定义存储后端。全项目仅有的两个接口之一。
// 以“本地文件路径”为出入参：OSS 走分片上传、本地后端直接拷贝。
package store

import (
	"context"
	"fmt"

	"Tide-Sync/internal/config"
)

// Backend 是存储后端的抽象。
type Backend interface {
	Put(ctx context.Context, key, localPath string) error
	Get(ctx context.Context, key, localPath string) error
	List(ctx context.Context, prefix string) ([]string, error)
}

// New 按配置显式选择后端；未知类型直接报错，不做隐式兜底。
func New(cfg config.Storage) (Backend, error) {
	switch cfg.Type {
	case "oss":
		return newOSS(cfg)
	case "local":
		return newLocal(cfg)
	default:
		return nil, fmt.Errorf("storage.type 无效: %q（可选 oss|local）", cfg.Type)
	}
}
