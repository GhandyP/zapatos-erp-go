package packaging

import (
	"testing"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

func TestServiceUpsertRejectsInvalidDimensions(t *testing.T) {
	svc := New(store.NewMemoryRepository([]PackagingSpec{}, func(v PackagingSpec) string { return v.ID }))
	_, err := svc.Upsert(PackagingSpec{ID: "pkg-1", Name: "Caja", TransportMode: "camion", MaxUnits: 10, Dimensions: domain.Dimensions{LengthCM: 0, WidthCM: 20, HeightCM: 10, WeightKG: 1}})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestServiceUpsertAndRemove(t *testing.T) {
	svc := New(store.NewMemoryRepository([]PackagingSpec{}, func(v PackagingSpec) string { return v.ID }))
	item, err := svc.Upsert(PackagingSpec{ID: "pkg-1", Name: "Caja", TransportMode: "camion", MaxUnits: 10, Dimensions: domain.Dimensions{LengthCM: 30, WidthCM: 20, HeightCM: 10, WeightKG: 1}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.ID != "pkg-1" {
		t.Fatalf("unexpected id: %s", item.ID)
	}
	if err := svc.Remove("pkg-1"); err != nil {
		t.Fatalf("unexpected remove error: %v", err)
	}
}
