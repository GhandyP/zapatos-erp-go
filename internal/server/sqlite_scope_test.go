package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"zapatos-erp-go/internal/audit"
	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/modules/billing"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/manufacturing"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/modules/rawmaterials"
	"zapatos-erp-go/internal/store"
)

func TestSQLiteTransactionScopeCommitsBusinessAndAuditTogether(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "scope.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	dimensions := domain.Dimensions{LengthCM: 1, WidthCM: 2, HeightCM: 3, WeightKG: 0.5}
	err = NewSQLiteTransactionScope(db).Run(context.Background(), func(services TransactionServices) error {
		if services.RawMaterials == nil || services.FinishedGoods == nil || services.Manufacturing == nil || services.Packaging == nil || services.Logistics == nil || services.Billing == nil || services.RecordAudit == nil {
			t.Fatal("SQLite transaction scope omitted a typed service or audit writer")
		}
		if _, err := services.RawMaterials.Upsert(rawMaterialForScopeTest("rm-1", dimensions)); err != nil {
			return err
		}
		if _, err := services.FinishedGoods.Upsert(finishedGoodForScopeTest("fg-1", dimensions)); err != nil {
			return err
		}
		if _, err := services.Packaging.Upsert(packagingForScopeTest("pkg-1", dimensions)); err != nil {
			return err
		}
		if _, err := services.Logistics.Upsert(logisticsForScopeTest("slot-1", dimensions)); err != nil {
			return err
		}
		if _, err := services.Billing.Upsert(invoiceForScopeTest("inv-1")); err != nil {
			return err
		}
		return services.RecordAudit("admin", "saved", "raw-materials:rm-1")
	})
	if err != nil {
		t.Fatalf("transaction scope Run returned error: %v", err)
	}

	for _, collection := range []string{
		store.CollectionRawMaterials,
		store.CollectionFinishedGoods,
		store.CollectionPackaging,
		store.CollectionLogistics,
		store.CollectionInvoices,
		store.CollectionAuditEvents,
	} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", collection).Scan(&count); err != nil {
			t.Fatalf("count collection %q: %v", collection, err)
		}
		if count != 1 {
			t.Errorf("collection %q row count = %d, want 1", collection, count)
		}
	}
	var payload string
	if err := db.QueryRow("SELECT payload FROM store_records WHERE collection = ?", store.CollectionAuditEvents).Scan(&payload); err != nil {
		t.Fatalf("read audit event: %v", err)
	}
	var event audit.Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("decode audit event: %v", err)
	}
	if event.Actor != "admin" || event.Action != "saved" || event.Entity != "raw-materials:rm-1" {
		t.Fatalf("committed audit event = %+v, want business mutation audit", event)
	}
}

func TestSQLiteTransactionScopeRollsBackBusinessAndAuditOnFailure(t *testing.T) {
	tests := []struct {
		name             string
		failAfterAudit   error
		wantAuditFailure bool
	}{
		{name: "callback error", failAfterAudit: errors.New("operation failed")},
		{name: "audit insert error", wantAuditFailure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "scope.sqlite"))
			if err != nil {
				t.Fatalf("open SQLite database: %v", err)
			}
			defer db.Close()
			if test.wantAuditFailure {
				if _, err := db.Exec(`CREATE TRIGGER reject_audit_event BEFORE INSERT ON store_records
					WHEN NEW.collection = 'audit_events'
					BEGIN SELECT RAISE(ABORT, 'audit rejected'); END`); err != nil {
					t.Fatalf("create audit rejection trigger: %v", err)
				}
			}

			err = NewSQLiteTransactionScope(db).Run(context.Background(), func(services TransactionServices) error {
				if _, err := services.RawMaterials.Upsert(rawMaterialForScopeTest("rm-rollback", domain.Dimensions{LengthCM: 1, WidthCM: 2, HeightCM: 3})); err != nil {
					return err
				}
				if err := services.RecordAudit("admin", "saved", "raw-materials:rm-rollback"); err != nil {
					return err
				}
				return test.failAfterAudit
			})
			if err == nil {
				t.Fatal("transaction scope Run returned nil, want failure")
			}

			for _, collection := range []string{store.CollectionRawMaterials, store.CollectionAuditEvents} {
				var count int
				if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", collection).Scan(&count); err != nil {
					t.Fatalf("count collection %q after rollback: %v", collection, err)
				}
				if count != 0 {
					t.Errorf("collection %q retained %d rows after rollback, want 0", collection, count)
				}
			}
		})
	}
}

func rawMaterialForScopeTest(id string, dimensions domain.Dimensions) rawmaterials.RawMaterial {
	return rawmaterials.RawMaterial{ID: id, Name: "Leather", Unit: "m2", MinStock: 1, Dimensions: dimensions}
}

func finishedGoodForScopeTest(id string, dimensions domain.Dimensions) finishedgoods.FinishedProductVariant {
	return finishedgoods.FinishedProductVariant{ID: id, Style: "Oxford", Size: "40", Color: "Black", Stock: 1, Dimensions: dimensions}
}

func packagingForScopeTest(id string, dimensions domain.Dimensions) packaging.PackagingSpec {
	return packaging.PackagingSpec{ID: id, Name: "Box", TransportMode: "truck", MaxUnits: 1, Dimensions: dimensions}
}

func logisticsForScopeTest(id string, dimensions domain.Dimensions) logistics.StorageSlot {
	return logistics.StorageSlot{ID: id, Name: "Rack A", Zone: "Z1", CapacityUnits: 1, Dimensions: dimensions}
}

func invoiceForScopeTest(id string) billing.Invoice {
	return billing.Invoice{ID: id, CustomerID: "customer-1", Total: 10, Tax: 2}
}

func TestSQLiteTransactionScopeCommitsManufacturingAndAuditTogether(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "manufacturing.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	scope := NewSQLiteTransactionScope(db)
	seedReleasedManufacturingOrder(t, scope)

	err = scope.Run(context.Background(), func(services TransactionServices) error {
		if _, err := services.Manufacturing.CompleteWorkOrder("wo-1", 2, "operator"); err != nil {
			return err
		}
		return services.RecordAudit("operator", "manufacturing.completed", "work-orders:wo-1")
	})
	if err != nil {
		t.Fatalf("complete manufacturing order: %v", err)
	}

	for collection, want := range map[string]int{
		store.CollectionManufacturingBOMs:       1,
		store.CollectionManufacturingWorkOrders: 1,
		store.CollectionMaterialMovements:       2,
		store.CollectionAuditEvents:             1,
	} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", collection).Scan(&count); err != nil {
			t.Fatalf("count collection %q: %v", collection, err)
		}
		if count != want {
			t.Errorf("collection %q row count = %d, want %d", collection, count, want)
		}
	}
	var finishedPayload string
	if err := db.QueryRow("SELECT payload FROM store_records WHERE collection = ? AND id = ?", store.CollectionFinishedGoods, "variant-1").Scan(&finishedPayload); err != nil {
		t.Fatalf("read finished variant: %v", err)
	}
	var variant finishedgoods.FinishedProductVariant
	if err := json.Unmarshal([]byte(finishedPayload), &variant); err != nil {
		t.Fatalf("decode finished variant: %v", err)
	}
	if variant.Stock != 6 {
		t.Errorf("finished stock = %d, want 6", variant.Stock)
	}
	var auditPayload string
	if err := db.QueryRow("SELECT payload FROM store_records WHERE collection = ?", store.CollectionAuditEvents).Scan(&auditPayload); err != nil {
		t.Fatalf("read committed audit event: %v", err)
	}
	var event audit.Event
	if err := json.Unmarshal([]byte(auditPayload), &event); err != nil {
		t.Fatalf("decode audit event: %v", err)
	}
	if event.Entity != "work-orders:wo-1" {
		t.Errorf("audit entity = %q, want work-orders:wo-1", event.Entity)
	}
}

func TestSQLiteTransactionScopeRollsBackManufacturingWhenAuditFails(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "manufacturing-rollback.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	scope := NewSQLiteTransactionScope(db)
	seedReleasedManufacturingOrder(t, scope)
	if _, err := db.Exec(`CREATE TRIGGER reject_audit_event BEFORE INSERT ON store_records
		WHEN NEW.collection = 'audit_events'
		BEGIN SELECT RAISE(ABORT, 'audit rejected'); END`); err != nil {
		t.Fatalf("create audit rejection trigger: %v", err)
	}

	err = scope.Run(context.Background(), func(services TransactionServices) error {
		if _, err := services.Manufacturing.CompleteWorkOrder("wo-1", 2, "operator"); err != nil {
			return err
		}
		return services.RecordAudit("operator", "manufacturing.completed", "work-orders:wo-1")
	})
	if err == nil {
		t.Fatal("transaction scope Run returned nil, want audit failure")
	}

	for collection, want := range map[string]int{
		store.CollectionManufacturingBOMs:       1,
		store.CollectionManufacturingWorkOrders: 1,
		store.CollectionMaterialMovements:       1,
		store.CollectionAuditEvents:             0,
	} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM store_records WHERE collection = ?", collection).Scan(&count); err != nil {
			t.Fatalf("count collection %q after rollback: %v", collection, err)
		}
		if count != want {
			t.Errorf("collection %q row count after rollback = %d, want %d", collection, count, want)
		}
	}
	var orderPayload string
	if err := db.QueryRow("SELECT payload FROM store_records WHERE collection = ? AND id = ?", store.CollectionManufacturingWorkOrders, "wo-1").Scan(&orderPayload); err != nil {
		t.Fatalf("read work order after rollback: %v", err)
	}
	var order manufacturing.WorkOrder
	if err := json.Unmarshal([]byte(orderPayload), &order); err != nil {
		t.Fatalf("decode work order after rollback: %v", err)
	}
	if order.Status != manufacturing.WorkOrderReleased || order.GoodQty != 0 {
		t.Errorf("work order after rollback = %+v, want unchanged released order", order)
	}
	var finishedPayload string
	if err := db.QueryRow("SELECT payload FROM store_records WHERE collection = ? AND id = ?", store.CollectionFinishedGoods, "variant-1").Scan(&finishedPayload); err != nil {
		t.Fatalf("read finished variant after rollback: %v", err)
	}
	var variant finishedgoods.FinishedProductVariant
	if err := json.Unmarshal([]byte(finishedPayload), &variant); err != nil {
		t.Fatalf("decode finished variant after rollback: %v", err)
	}
	if variant.Stock != 4 {
		t.Errorf("finished stock after rollback = %d, want 4", variant.Stock)
	}
}

func seedReleasedManufacturingOrder(t *testing.T, scope TransactionScope) {
	t.Helper()
	err := scope.Run(context.Background(), func(services TransactionServices) error {
		dimensions := domain.Dimensions{LengthCM: 1, WidthCM: 2, HeightCM: 3, WeightKG: 0.5}
		if _, err := services.RawMaterials.Upsert(rawMaterialForScopeTest("leather", dimensions)); err != nil {
			return err
		}
		variant := finishedGoodForScopeTest("variant-1", dimensions)
		variant.Stock = 4
		if _, err := services.FinishedGoods.Upsert(variant); err != nil {
			return err
		}
		if _, err := services.Manufacturing.SaveBOM(manufacturing.BOM{
			ID: "bom-1", Revision: 1, VariantID: "variant-1", MaterialID: "leather", QuantityPerUnit: 2, Unit: "m2",
		}, "admin"); err != nil {
			return err
		}
		if _, err := services.Manufacturing.OpenStock("leather", "m2", 20, "admin"); err != nil {
			return err
		}
		if _, err := services.Manufacturing.CreateWorkOrder("wo-1", "bom-1", 3, "planner"); err != nil {
			return err
		}
		_, err := services.Manufacturing.ReleaseWorkOrder("wo-1", "supervisor")
		return err
	})
	if err != nil {
		t.Fatalf("seed released manufacturing order: %v", err)
	}
}
