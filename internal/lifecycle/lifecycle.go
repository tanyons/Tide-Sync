// Package lifecycle 生成并（可选）下发 OSS 生命周期规则。
// 唯一职责：过期删除 + 清理未完成分片。天数由配置提供（lifecycle.*）。
package lifecycle

import (
	"fmt"
	"io"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"Tide-Sync/internal/config"
	"Tide-Sync/internal/store"
)

// RuleID 是本工具生命周期规则的固定 ID。
func RuleID(days int) string { return fmt.Sprintf("tide-sync-expire-%dd", days) }

// XML 生成生命周期规则的 XML 文本（供用户复制到控制台或 ossutil）。
func XML(prefix string, days, abortDays int) string {
	return fmt.Sprintf(`<LifecycleConfiguration>
  <Rule>
    <ID>%s</ID>
    <Prefix>%s/</Prefix>
    <Status>Enabled</Status>
    <Expiration><Days>%d</Days></Expiration>
    <AbortMultipartUpload><DaysAfterInitiation>%d</DaysAfterInitiation></AbortMultipartUpload>
  </Rule>
</LifecycleConfiguration>
`, RuleID(days), prefix, days, abortDays)
}

// Print 输出 XML 与操作说明（默认行为，不改动桶）。
func Print(w io.Writer, cfg *config.Config, days, abortDays int) {
	prefix := cfg.Storage.Prefix
	fmt.Fprint(w, XML(prefix, days, abortDays))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "操作方式（任选其一，只需执行一次）：")
	fmt.Fprintln(w, "  1) 阿里云 OSS 控制台 -> Bucket -> 数据管理 -> 生命周期管理 -> 按上面填写")
	fmt.Fprintln(w, "  2) ossutil：把上面的 XML 保存为 lifecycle.xml 后执行：")
	fmt.Fprintf(w, "     ossutil lifecycle --method put oss://%s lifecycle.xml\n", cfg.Storage.Bucket)
}

// Apply 通过 SDK 下发规则。先读取桶上已有规则、替换同名规则后整体写回，避免清掉其它规则。
func Apply(cfg *config.Config, days, abortDays int) error {
	endpoint := cfg.Storage.Endpoint
	if cfg.Storage.InternalEndpoint {
		endpoint = store.InternalEndpoint(endpoint)
	}
	client, err := oss.New(endpoint, cfg.Storage.AccessKeyID, cfg.Storage.AccessKeySecret)
	if err != nil {
		return fmt.Errorf("创建 OSS client 失败: %w", err)
	}

	ourID := RuleID(days)
	var rules []oss.LifecycleRule
	if existing, gerr := client.GetBucketLifecycle(cfg.Storage.Bucket); gerr == nil {
		for _, r := range existing.Rules {
			if r.ID != ourID {
				rules = append(rules, r)
			}
		}
	}
	rules = append(rules, oss.LifecycleRule{
		ID:                   ourID,
		Prefix:               cfg.Storage.Prefix + "/",
		Status:               "Enabled",
		Expiration:           &oss.LifecycleExpiration{Days: days},
		AbortMultipartUpload: &oss.LifecycleAbortMultipartUpload{Days: abortDays},
	})

	if err := client.SetBucketLifecycle(cfg.Storage.Bucket, rules); err != nil {
		return fmt.Errorf("下发生命周期规则失败: %w", err)
	}
	return nil
}
