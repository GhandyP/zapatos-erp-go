package rawmaterials

import (
	"testing"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

func TestServiceUpsertValidatesDimensions(t *testing.T) {
	svc := New(store.NewMemoryRepository([]RawMaterial{}, func(v RawMaterial) string { return v.ID }))
	_, err := svc.Upsert(RawMaterial{ID: "rm-1", Name: "Cuero", Unit: "m2", MinStock: 2, Dimensions: domain.Dimensions{LengthCM: 0, WidthCM: 5, HeightCM: 2, WeightKG: 1}})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestServiceUpsertAcceptsDimensions(t *testing.T) {
	svc := New(store.NewMemoryRepository([]RawMaterial{}, func(v RawMaterial) string { return v.ID }))
	item, err := svc.Upsert(RawMaterial{ID: "rm-1", Name: "Cuero", Unit: "m2", MinStock: 2, Dimensions: domain.Dimensions{LengthCM: 10, WidthCM: 5, HeightCM: 2, WeightKG: 1}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Dimensions.LengthCM != 10 {
		t.Fatalf("unexpected dimensions: %+v", item.Dimensions)
	}
}
