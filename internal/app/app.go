package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/server"
	"zapatos-erp-go/internal/web"
)

func Run() (runErr error) {
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
	runtime, err := buildSQLiteRuntime(ctx, cfg.DataDir, cfg.SessionUsers())
	if err != nil {
		return err
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close SQLite runtime: %w", err))
		}
	}()

	return server.RunWithTransactionScope(ctx, cfg.BindAddr, runtime.modules, runtime.sessions, runtime.auditStore, web.NewUI(), runtime.transactionScope)
}

func validateStartupInputs(cfg BootstrapConfig) error {
	if strings.TrimSpace(cfg.BindAddr) == "" {
		return fmt.Errorf("bind address is required")
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return fmt.Errorf("data directory is required")
	}
	info, err := os.Stat(cfg.DataDir)
	if err == nil && !info.IsDir() {
		return fmt.Errorf("data directory %q is not a directory", cfg.DataDir)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect data directory: %w", err)
	}
	return nil
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
