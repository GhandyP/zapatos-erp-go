package logistics

import (
	"testing"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

func TestServiceUpsertRejectsInvalidDimensions(t *testing.T) {
	svc := New(store.NewMemoryRepository([]StorageSlot{}, func(v StorageSlot) string { return v.ID }))
	_, err := svc.Upsert(StorageSlot{ID: "slot-1", Name: "Rack A", Zone: "Z1", CapacityUnits: 8, Dimensions: domain.Dimensions{LengthCM: 10, WidthCM: 0, HeightCM: 10, WeightKG: 1}})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestServiceUpsertAndRemove(t *testing.T) {
	svc := New(store.NewMemoryRepository([]StorageSlot{}, func(v StorageSlot) string { return v.ID }))
	item, err := svc.Upsert(StorageSlot{ID: "slot-1", Name: "Rack A", Zone: "Z1", CapacityUnits: 8, Dimensions: domain.Dimensions{LengthCM: 10, WidthCM: 12, HeightCM: 14, WeightKG: 1}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Zone != "Z1" {
		t.Fatalf("unexpected zone: %s", item.Zone)
	}
	if err := svc.Remove("slot-1"); err != nil {
		t.Fatalf("unexpected remove error: %v", err)
	}
}
