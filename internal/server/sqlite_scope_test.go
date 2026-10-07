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
		if services.RawMaterials == nil || services.FinishedGoods == nil || services.Packaging == nil || services.Logistics == nil || services.Billing == nil || services.RecordAudit == nil {
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
