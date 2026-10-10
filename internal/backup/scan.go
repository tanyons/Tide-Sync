package backup

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// File 是扫描到的一个文件。
type File struct {
	Rel   string // 相对 source 的斜杠路径
	Abs   string // 绝对路径
	Size  int64
	MTime time.Time
}

// ScanOptions 控制扫描行为。
type ScanOptions struct {
	Exclude        []string        // 排除规则（doublestar 语法）
	FollowSymlinks bool            // 是否跟随符号链接
	SkipDone       bool            // 是否跳过已完成的日期目录
	DoneDateDirs   map[string]bool // 已完成的日期目录名
}

// Scan 遍历 source，返回所有应纳入备份的文件。
func Scan(root string, opt ScanOptions) ([]File, error) {
	var files []File
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			// 顶层、已完成的日期目录：整目录跳过（优化）
			if opt.SkipDone && !strings.Contains(rel, "/") && opt.DoneDateDirs[rel] {
				return fs.SkipDir
			}
			if shouldExclude(rel, opt.Exclude) {
				return fs.SkipDir
			}
			return nil
		}

		if shouldExclude(rel, opt.Exclude) {
			return nil
		}

		isSymlink := d.Type()&fs.ModeSymlink != 0
		if isSymlink && !opt.FollowSymlinks {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if isSymlink {
			ti, terr := os.Stat(p) // 跟随符号链接取目标信息
			if terr != nil {
				return nil // 断链，跳过
			}
			info = ti
		}
		if info.IsDir() {
			return nil
		}

		files = append(files, File{Rel: rel, Abs: p, Size: info.Size(), MTime: info.ModTime()})
		return nil
	})
	return files, err
}

// shouldExclude 是全项目唯一实现排除匹配的地方。
func shouldExclude(rel string, patterns []string) bool {
	base := path.Base(rel)
	for _, p := range patterns {
		if ok, _ := doublestar.Match(p, rel); ok {
			return true
		}
		if !strings.Contains(p, "/") {
			if ok, _ := doublestar.Match(p, base); ok {
				return true
			}
		}
	}
	return false
}

var dateInName = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// ParseDateDir 是全项目唯一识别日期目录的地方。
// name 需先匹配 re；再从名字里取出首个 YYYY-MM-DD 作为日期。
func ParseDateDir(name string, re *regexp.Regexp) (time.Time, bool) {
	if re != nil && !re.MatchString(name) {
		return time.Time{}, false
	}
	m := dateInName.FindString(name)
	if m == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", m, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
