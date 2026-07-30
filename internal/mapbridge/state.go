package mapbridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

type fileState struct {
	WrittenHash string `json:"written_hash,omitempty"`
	SeenHash    string `json:"seen_hash,omitempty"`
	TaskHash    string `json:"task_hash,omitempty"`
}
type claimState struct {
	Foreign string `json:"foreign"`
	Was     string `json:"was"`
}
type synthesizedState struct {
	Answer []int `json:"answer,omitempty"`
	MapMD  bool  `json:"map_md,omitempty"`
}
type bridgeState struct {
	Repo        string                `json:"repo"`
	Numbers     map[string]int        `json:"numbers"`
	Retired     []int                 `json:"retired,omitempty"`
	Files       map[string]fileState  `json:"files"`
	Claims      map[string]claimState `json:"claims"`
	Synthesized synthesizedState      `json:"synthesized"`
	MapTaskHash string                `json:"map_task_hash,omitempty"`
}

func newState(repo string) bridgeState {
	return bridgeState{Repo: repo, Numbers: map[string]int{}, Files: map[string]fileState{}, Claims: map[string]claimState{}}
}
func loadState(path, repo string) (bridgeState, error) {
	st := newState(repo)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err = json.Unmarshal(data, &st); err != nil {
		return st, err
	}
	if st.Numbers == nil {
		st.Numbers = map[string]int{}
	}
	if st.Files == nil {
		st.Files = map[string]fileState{}
	}
	if st.Claims == nil {
		st.Claims = map[string]claimState{}
	}
	st.Repo = repo
	return st, nil
}
func saveState(path string, st bridgeState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if existing, readErr := os.ReadFile(path); readErr == nil && string(existing) == string(data) {
		return nil
	}
	return atomicWrite(path, data, 0644)
}
func stateFileKey(n int) string { return strconv.Itoa(n) }
func containsNumber(ns []int, n int) bool {
	for _, v := range ns {
		if v == n {
			return true
		}
	}
	return false
}
func appendNumber(ns []int, n int) []int {
	if containsNumber(ns, n) {
		return ns
	}
	return append(ns, n)
}
func statePath(base, id string) string { return filepath.Join(base, "bridge", id+".json") }
