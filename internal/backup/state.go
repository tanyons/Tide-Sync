package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// FileState 记录单个文件上次成功上传时的指纹。
type FileState struct {
	Size  int64 `json:"size"`
	MTime int64 `json:"mtime"` // UnixNano
}

// SourceState 是单个源目录的状态。
type SourceState struct {
	Files        map[string]FileState `json:"files"` // rel 路径 -> 指纹
	LastRun      time.Time            `json:"last_run"`
	DoneDateDirs []string             `json:"done_date_dirs,omitempty"` // 已完成、可跳过遍历的日期目录
}

// State 是整个状态账本，按“源路径对应的桶内路径段”隔离（见 SourcePrefix）。
type State struct {
	Sources map[string]*SourceState `json:"sources"`
}

// LoadState 读取状态文件；文件不存在视为空状态（首次运行）。
func LoadState(path string) (*State, error) {
	st := &State{Sources: map[string]*SourceState{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	if st.Sources == nil {
		st.Sources = map[string]*SourceState{}
	}
	return st, nil
}

// Source 取得（必要时创建）某个源的状态。
func (st *State) Source(key string) *SourceState {
	s, ok := st.Sources[key]
	if !ok {
		s = &SourceState{Files: map[string]FileState{}}
		st.Sources[key] = s
	}
	if s.Files == nil {
		s.Files = map[string]FileState{}
	}
	return s
}

// Save 原子写回状态文件（先写临时文件再 rename）。
func (st *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
