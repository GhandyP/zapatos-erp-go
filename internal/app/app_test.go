package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/packaging"
)

func TestBuildModulesUsesSeedFallbacksWhenFilesMissing(t *testing.T) {
	dir := t.TempDir()
	modules := buildModules(filepath.Join(dir, "data"))

	if modules == nil || modules.Packaging == nil || modules.Logistics == nil {
		t.Fatal("expected packaging and logistics modules to be initialized")
	}

	pkgItems, err := modules.Packaging.List()
	if err != nil {
		t.Fatalf("packaging list failed: %v", err)
	}
	if len(pkgItems) != 1 || pkgItems[0].ID != "pkg-1" {
		t.Fatalf("unexpected packaging seeds: %#v", pkgItems)
	}

	logItems, err := modules.Logistics.List()
	if err != nil {
		t.Fatalf("logistics list failed: %v", err)
	}
	if len(logItems) != 1 || logItems[0].ID != "slot-1" {
		t.Fatalf("unexpected logistics seeds: %#v", logItems)
	}
}

func TestBuildModulesLoadsPackagedAndLogisticsFiles(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	writeJSON(t, filepath.Join(dataDir, "packaging.json"), []packaging.PackagingSpec{{ID: "pkg-x", Name: "Caja chica", TransportMode: "moto", MaxUnits: 8}})
	writeJSON(t, filepath.Join(dataDir, "logistics.json"), []logistics.StorageSlot{{ID: "slot-x", Name: "Rack B", Zone: "Z2", CapacityUnits: 9}})

	modules := buildModules(dataDir)

	pkgItems, err := modules.Packaging.List()
	if err != nil {
		t.Fatalf("packaging list failed: %v", err)
	}
	if len(pkgItems) != 1 || pkgItems[0].ID != "pkg-x" {
		t.Fatalf("unexpected packaging items: %#v", pkgItems)
	}

	logItems, err := modules.Logistics.List()
	if err != nil {
		t.Fatalf("logistics list failed: %v", err)
	}
	if len(logItems) != 1 || logItems[0].ID != "slot-x" {
		t.Fatalf("unexpected logistics items: %#v", logItems)
	}
}

func TestValidateStartupInputsRejectsFileDataDir(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.WriteFile(dataDir, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	err := validateStartupInputs(BootstrapConfig{BindAddr: ":3001", DataDir: dataDir})
	if err == nil {
		t.Fatal("expected startup validation to fail for a file path")
	}
}

func writeJSON(t *testing.T, path string, data any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
