package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"zapatos-erp-go/internal/audit"
	"zapatos-erp-go/internal/auth"
	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/integrations/foxpro"
	"zapatos-erp-go/internal/modules/billing"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/modules/rawmaterials"
	"zapatos-erp-go/internal/server"
	"zapatos-erp-go/internal/store"
	"zapatos-erp-go/internal/web"
)

func Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg := LoadBootstrapConfig(wd)
	if err := validateStartupInputs(cfg); err != nil {
		return err
	}
	modules := buildModules(cfg.DataDir)
	sessions := auth.NewSessionStore(filepath.Join(cfg.DataDir, "sessions.json"), cfg.SessionUsers())
	auditStore := audit.NewStore(filepath.Join(cfg.DataDir, "audit.json"))
	ui := web.NewUI()
	return server.Run(ctx, cfg.BindAddr, modules, sessions, auditStore, ui)
}

func validateStartupInputs(cfg BootstrapConfig) error {
	if strings.TrimSpace(cfg.BindAddr) == "" {
		return fmt.Errorf("bind address is required")
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return fmt.Errorf("data directory is required")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("prepare data directory: %w", err)
	}
	probe, err := os.CreateTemp(cfg.DataDir, ".writable-*")
	if err != nil {
		return fmt.Errorf("data directory is not writable: %w", err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close data directory probe: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("cleanup data directory probe: %w", err)
	}
	return nil
}

func buildModules(dataDir string) *server.Modules {
	rawRepo := store.NewJSONFileRepository(filepath.Join(dataDir, "raw-materials.json"), []rawmaterials.RawMaterial{}, func(v rawmaterials.RawMaterial) string { return v.ID })
	finishedRepo := store.NewJSONFileRepository(filepath.Join(dataDir, "finished-goods.json"), []finishedgoods.FinishedProductVariant{}, func(v finishedgoods.FinishedProductVariant) string { return v.ID })
	packagingRepo := store.NewJSONFileRepository(filepath.Join(dataDir, "packaging.json"), defaultPackagingSeeds(), func(v packaging.PackagingSpec) string { return v.ID })
	logisticsRepo := store.NewJSONFileRepository(filepath.Join(dataDir, "logistics.json"), defaultLogisticsSeeds(), func(v logistics.StorageSlot) string { return v.ID })
	billingRepo := store.NewJSONFileRepository(filepath.Join(dataDir, "invoices.json"), []billing.Invoice{}, func(v billing.Invoice) string { return v.ID })
	rawSvc := rawmaterials.New(rawRepo)
	finishedSvc := finishedgoods.New(finishedRepo)
	packagingSvc := packaging.New(packagingRepo)
	logisticsSvc := logistics.New(logisticsRepo)
	billingSvc := billing.New(billingRepo)
	foxproAdapter := foxpro.New(billingSvc)
	return &server.Modules{RawMaterials: rawSvc, FinishedGoods: finishedSvc, Packaging: packagingSvc, Logistics: logisticsSvc, Billing: billingSvc, Foxpro: foxproAdapter}
}

func defaultPackagingSeeds() []packaging.PackagingSpec {
	return []packaging.PackagingSpec{{
		ID:            "pkg-1",
		Name:          "Caja estandar",
		TransportMode: "camion",
		MaxUnits:      20,
		Dimensions:    domain.Dimensions{LengthCM: 40, WidthCM: 30, HeightCM: 25, WeightKG: 1.2},
	}}
}

func defaultLogisticsSeeds() []logistics.StorageSlot {
	return []logistics.StorageSlot{{
		ID:            "slot-1",
		Name:          "Rack A",
		Zone:          "Z1",
		CapacityUnits: 5,
		Dimensions:    domain.Dimensions{LengthCM: 100, WidthCM: 60, HeightCM: 180, WeightKG: 12},
	}}
}
