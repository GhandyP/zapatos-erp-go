package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadBootstrapConfig(t *testing.T) {
	tests := []struct {
		name      string
		configure func(t *testing.T, root string)
		wantBind  string
		wantData  string
		wantSeeds []string
	}{
		{
			name: "uses defaults when bootstrap file is missing",
			configure: func(t *testing.T, root string) {
				t.Helper()
			},
			wantBind: defaultBindAddr,
			wantData: "data",
			wantSeeds: []string{
				"admin", "warehouse", "sales", "accounting", "audit", "production",
			},
		},
		{
			name: "applies partial bootstrap values and keeps defaults",
			configure: func(t *testing.T, root string) {
				t.Helper()
				writeBootstrapConfig(t, root, BootstrapConfig{BindAddr: ":8181", DataDir: "store"})
			},
			wantBind: ":8181",
			wantData: "store",
			wantSeeds: []string{
				"admin", "warehouse", "sales", "accounting", "audit", "production",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.configure(t, root)

			cfg := LoadBootstrapConfig(root)

			if cfg.BindAddr != tt.wantBind {
				t.Fatalf("BindAddr = %q, want %q", cfg.BindAddr, tt.wantBind)
			}
			if cfg.DataDir != resolveBootstrapPath(root, tt.wantData) {
				t.Fatalf("DataDir = %q, want %q", cfg.DataDir, resolveBootstrapPath(root, tt.wantData))
			}

			gotUsers := make([]string, 0, len(cfg.SeedAccounts))
			for _, seed := range cfg.SeedAccounts {
				gotUsers = append(gotUsers, seed.Username)
			}
			if !reflect.DeepEqual(gotUsers, tt.wantSeeds) {
				t.Fatalf("SeedAccounts usernames = %#v, want %#v", gotUsers, tt.wantSeeds)
			}
		})
	}
}

func writeBootstrapConfig(t *testing.T, root string, cfg BootstrapConfig) {
	t.Helper()
	path := filepath.Join(root, "config", "bootstrap.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal bootstrap config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write bootstrap config: %v", err)
	}
}
