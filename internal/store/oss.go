package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"Tide-Sync/internal/config"
)

// ossStore 是阿里云 OSS 后端。
type ossStore struct {
	bucket       *oss.Bucket
	listPageSize int
}

// OSSClient 按配置创建 OSS client；internal endpoint 转换在这里统一处理。
func OSSClient(cfg config.Storage) (*oss.Client, error) {
	endpoint := cfg.Endpoint
	if cfg.InternalEndpoint {
		endpoint = InternalEndpoint(endpoint)
	}
	client, err := oss.New(endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("创建 OSS client 失败: %w", err)
	}
	return client, nil
}

func newOSS(cfg config.Storage) (Backend, error) {
	client, err := OSSClient(cfg)
	if err != nil {
		return nil, err
	}
	bucket, err := client.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("获取 bucket 失败: %w", err)
	}
	return &ossStore{bucket: bucket, listPageSize: cfg.ListPageSize}, nil
}

// InternalEndpoint 把公网 endpoint 转成内网 endpoint：
// oss-cn-hangzhou.aliyuncs.com -> oss-cn-hangzhou-internal.aliyuncs.com
func InternalEndpoint(ep string) string {
	if strings.Contains(ep, "-internal.") {
		return ep
	}
	if i := strings.Index(ep, ".aliyuncs.com"); i > 0 {
		return ep[:i] + "-internal" + ep[i:]
	}
	return ep
}

func (s *ossStore) Put(_ context.Context, key, localPath string) error {
	return s.bucket.PutObjectFromFile(key, localPath)
}

func (s *ossStore) Get(_ context.Context, key, localPath string) error {
	return s.bucket.GetObjectToFile(key, localPath)
}

func (s *ossStore) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	marker := oss.Marker("")
	for {
		res, err := s.bucket.ListObjects(oss.Prefix(prefix), oss.MaxKeys(s.listPageSize), marker)
		if err != nil {
			return nil, err
		}
		for _, o := range res.Objects {
			out = append(out, o.Key)
		}
		if !res.IsTruncated {
			break
		}
		marker = oss.Marker(res.NextMarker)
	}
	return out, nil
}
