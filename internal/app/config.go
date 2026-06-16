package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"zapatos-erp-go/internal/auth"
)

const defaultBindAddr = ":3000"

type BootstrapConfig struct {
	BindAddr     string        `json:"bindAddr"`
	DataDir      string        `json:"dataDir"`
	SeedAccounts []SeedAccount `json:"seedAccounts"`
}

type SeedAccount struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func LoadBootstrapConfig(root string) BootstrapConfig {
	cfg := BootstrapConfig{
		BindAddr:     defaultBindAddr,
		DataDir:      filepath.Join(root, "data"),
		SeedAccounts: defaultSeedAccounts(),
	}

	data, err := os.ReadFile(filepath.Join(root, "config", "bootstrap.json"))
	if err != nil {
		return cfg
	}

	var file BootstrapConfig
	if err := json.Unmarshal(data, &file); err != nil {
		return cfg
	}

	if bindAddr := strings.TrimSpace(file.BindAddr); bindAddr != "" {
		cfg.BindAddr = bindAddr
	}
	if dataDir := strings.TrimSpace(file.DataDir); dataDir != "" {
		cfg.DataDir = resolveBootstrapPath(root, dataDir)
	}
	if len(file.SeedAccounts) > 0 {
		if seeds := normalizeSeedAccounts(file.SeedAccounts); len(seeds) > 0 {
			cfg.SeedAccounts = seeds
		}
	}

	return cfg
}

func (c BootstrapConfig) SessionUsers() []auth.UserAccount {
	users := make([]auth.UserAccount, 0, len(c.SeedAccounts))
	for _, seed := range c.SeedAccounts {
		if strings.TrimSpace(seed.Username) == "" || strings.TrimSpace(seed.Password) == "" || strings.TrimSpace(seed.Role) == "" {
			continue
		}
		users = append(users, auth.NewUserAccount(seed.Username, seed.Password, seed.Role))
	}
	return users
}

func defaultSeedAccounts() []SeedAccount {
	return []SeedAccount{
		{Username: "admin", Password: "admin123", Role: "administrador"},
		{Username: "warehouse", Password: "warehouse123", Role: "almacen"},
		{Username: "sales", Password: "sales123", Role: "ventas"},
		{Username: "accounting", Password: "accounting123", Role: "contabilidad"},
		{Username: "audit", Password: "audit123", Role: "auditoria"},
		{Username: "production", Password: "production123", Role: "produccion"},
	}
}

func normalizeSeedAccounts(seeds []SeedAccount) []SeedAccount {
	out := make([]SeedAccount, 0, len(seeds))
	for _, seed := range seeds {
		if strings.TrimSpace(seed.Username) == "" || strings.TrimSpace(seed.Password) == "" || strings.TrimSpace(seed.Role) == "" {
			continue
		}
		out = append(out, seed)
	}
	if len(out) == 0 {
		return defaultSeedAccounts()
	}
	return out
}

func resolveBootstrapPath(root, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(root, value)
}
