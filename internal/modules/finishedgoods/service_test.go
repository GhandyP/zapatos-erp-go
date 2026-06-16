package finishedgoods

import (
	"testing"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

func TestServiceUpsertValidatesDimensions(t *testing.T) {
	svc := New(store.NewMemoryRepository([]FinishedProductVariant{}, func(v FinishedProductVariant) string { return v.ID }))
	_, err := svc.Upsert(FinishedProductVariant{Style: "Oxford", Size: "40", Color: "Negro", Stock: 1, Dimensions: domain.Dimensions{LengthCM: -1, WidthCM: 8, HeightCM: 12, WeightKG: 0.8}})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestServiceUpsertAcceptsDimensions(t *testing.T) {
	svc := New(store.NewMemoryRepository([]FinishedProductVariant{}, func(v FinishedProductVariant) string { return v.ID }))
	item, err := svc.Upsert(FinishedProductVariant{Style: "Oxford", Size: "40", Color: "Negro", Stock: 1, Dimensions: domain.Dimensions{LengthCM: 28, WidthCM: 10, HeightCM: 12, WeightKG: 0.8}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.ID == "" {
		t.Fatal("expected generated ID")
	}
}
