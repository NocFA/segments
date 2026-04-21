package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigParsesJSONLExport(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-cfg-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cfgPath := filepath.Join(dir, "config.yaml")

	body := []byte(`port: "8765"
bind: "127.0.0.1"
data_dir: "~/.segments"
jsonl_export:
  enabled: true
  path: ".segments/tasks.jsonl"
  scope: project
  project_id: "abc"
  on_events: [created, done]
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.JSONLExport.Enabled {
		t.Error("expected JSONLExport.Enabled=true")
	}
	if cfg.JSONLExport.Path != ".segments/tasks.jsonl" {
		t.Errorf("Path = %q", cfg.JSONLExport.Path)
	}
	if cfg.JSONLExport.Scope != "project" {
		t.Errorf("Scope = %q", cfg.JSONLExport.Scope)
	}
	if cfg.JSONLExport.ProjectID != "abc" {
		t.Errorf("ProjectID = %q", cfg.JSONLExport.ProjectID)
	}
	if len(cfg.JSONLExport.OnEvents) != 2 {
		t.Errorf("OnEvents = %v", cfg.JSONLExport.OnEvents)
	}
}

func TestLoadConfigWithoutJSONLExport(t *testing.T) {
	dir, err := os.MkdirTemp("", "segments-cfg-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cfgPath := filepath.Join(dir, "config.yaml")

	body := []byte(`port: "9000"
bind: "127.0.0.1"
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JSONLExport.Enabled {
		t.Error("expected Enabled=false by default")
	}
	if cfg.Port != "9000" {
		t.Errorf("Port = %q", cfg.Port)
	}
}
