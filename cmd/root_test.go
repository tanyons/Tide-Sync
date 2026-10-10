package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("config", "", "")
	return c
}

func TestResolveConfigPathExplicit(t *testing.T) {
	c := newTestCmd()
	if err := c.Flags().Set("config", "/tmp/x.toml"); err != nil {
		t.Fatal(err)
	}
	got, err := resolveConfigPath(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/x.toml" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveConfigPathFromCWD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	got, err := resolveConfigPath(newTestCmd())
	if err != nil {
		t.Fatal(err)
	}
	if got != "config.toml" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveConfigPathNotFound(t *testing.T) {
	t.Chdir(t.TempDir()) // 空目录，且可执行文件同目录也没有 config.toml
	if _, err := resolveConfigPath(newTestCmd()); err == nil {
		t.Fatal("找不到配置时应报错")
	}
}
