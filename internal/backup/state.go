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

// JobState 是单个任务的状态。
type JobState struct {
	Files        map[string]FileState `json:"files"` // rel 路径 -> 指纹
	LastRun      time.Time            `json:"last_run"`
	DoneDateDirs []string             `json:"done_date_dirs,omitempty"` // 已完成、可跳过遍历的日期目录
}

// State 是整个状态账本，按 job 名隔离。
type State struct {
	Jobs map[string]*JobState `json:"jobs"`
}

// LoadState 读取状态文件；文件不存在视为空状态（首次运行）。
func LoadState(path string) (*State, error) {
	st := &State{Jobs: map[string]*JobState{}}
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
	if st.Jobs == nil {
		st.Jobs = map[string]*JobState{}
	}
	return st, nil
}

// Job 取得（必要时创建）某个任务的子状态。
func (st *State) Job(name string) *JobState {
	j, ok := st.Jobs[name]
	if !ok {
		j = &JobState{Files: map[string]FileState{}}
		st.Jobs[name] = j
	}
	if j.Files == nil {
		j.Files = map[string]FileState{}
	}
	return j
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
