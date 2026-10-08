package manufacturing

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/rawmaterials"
	"zapatos-erp-go/internal/store"
)

func TestBOMRejectsFractionalQuantityPerUnitJSON(t *testing.T) {
	var bom BOM
	if err := json.Unmarshal([]byte(`{"quantityPerUnit":1.5}`), &bom); err == nil {
		t.Fatal("fractional quantityPerUnit decoded successfully, want integer-domain rejection")
	}
}

func TestMaterialMovementRejectsFractionalQuantityJSON(t *testing.T) {
	var movement MaterialMovement
	if err := json.Unmarshal([]byte(`{"quantity":1.5}`), &movement); err == nil {
		t.Fatal("fractional quantity decoded successfully, want integer-domain rejection")
	}
}

func TestCompleteWorkOrderRejectsConsumptionOverflowWithoutChangingState(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	service, _, finished := newManufacturingFixture(t, 0)
	if _, err := service.SaveBOM(BOM{ID: "bom-1", Revision: 1, VariantID: "variant-1", MaterialID: "leather", QuantityPerUnit: 2, Unit: "m2"}, "admin"); err != nil {
		t.Fatalf("save BOM: %v", err)
	}
	if _, err := service.OpenStock("leather", "m2", maxInt, "admin"); err != nil {
		t.Fatalf("open material stock: %v", err)
	}
	if _, err := service.CreateWorkOrder("wo-1", "bom-1", maxInt, "planner"); err != nil {
		t.Fatalf("create work order: %v", err)
	}
	if _, err := service.ReleaseWorkOrder("wo-1", "supervisor"); err != nil {
		t.Fatalf("release work order: %v", err)
	}

	before := snapshotManufacturingState(t, service, finished)
	if _, err := service.CompleteWorkOrder("wo-1", 1, "operator"); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("completion error = %v, want integer consumption overflow", err)
	}
	after := snapshotManufacturingState(t, service, finished)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("state changed after overflow rejection:\nbefore: %+v\nafter:  %+v", before, after)
	}
}

func TestMaterialOnHandRejectsOverflowingStoredMovements(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	service, _, _ := newManufacturingFixture(t, 0)
	service.movements = store.NewMemoryRepository([]MaterialMovement{
		{ID: "opening-1", MaterialID: "leather", Unit: "m2", Quantity: maxInt, Kind: MovementOpeningStock},
		{ID: "opening-2", MaterialID: "leather", Unit: "m2", Quantity: 1, Kind: MovementOpeningStock},
	}, func(item MaterialMovement) string { return item.ID })

	if _, err := service.MaterialOnHand("leather"); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("MaterialOnHand error = %v, want movement accumulation overflow", err)
	}
}

func TestWorkOrderCompletionConsumesPlannedMaterialAndAddsGoodStock(t *testing.T) {
	service, raw, finished := newManufacturingFixture(t, 4)
	if _, err := service.SaveBOM(BOM{ID: "bom-1", Revision: 1, VariantID: "variant-1", MaterialID: "leather", QuantityPerUnit: 2, Unit: "m2"}, "admin"); err != nil {
		t.Fatalf("save BOM: %v", err)
	}
	if _, err := service.OpenStock("leather", "m2", 20, "admin"); err != nil {
		t.Fatalf("open material stock: %v", err)
	}
	order, err := service.CreateWorkOrder("wo-1", "bom-1", 3, "planner")
	if err != nil {
		t.Fatalf("create work order: %v", err)
	}
	if order.Status != WorkOrderDraft || order.BOMRevision != 1 || order.CreatedBy != "planner" || order.CreatedAt == "" {
		t.Fatalf("created work order = %+v, want draft pinned to BOM revision 1 with actor and timestamp", order)
	}
	if _, err := service.ReleaseWorkOrder("wo-1", "supervisor"); err != nil {
		t.Fatalf("release work order: %v", err)
	}
	completed, err := service.CompleteWorkOrder("wo-1", 2, "operator")
	if err != nil {
		t.Fatalf("complete work order: %v", err)
	}
	if completed.Status != WorkOrderCompleted || completed.GoodQty != 2 || completed.CompletedBy != "operator" || completed.CompletedAt == "" {
		t.Fatalf("completed work order = %+v, want completed with two good units and completion actor/timestamp", completed)
	}
	movements, err := service.ListMaterialMovements()
	if err != nil {
		t.Fatalf("list material movements: %v", err)
	}
	if len(movements) != 2 {
		t.Fatalf("movement count = %d, want opening and consumption", len(movements))
	}
	var consumed MaterialMovement
	for _, movement := range movements {
		if movement.Kind == MovementConsumption {
			consumed = movement
		}
	}
	if consumed.ID == "" || consumed.Quantity != 6 || consumed.Unit != "m2" || consumed.WorkOrderID != "wo-1" || consumed.Actor != "operator" {
		t.Fatalf("consumption movement = %+v, want 6 m2 linked to wo-1", consumed)
	}
	if onHand, err := service.MaterialOnHand("leather"); err != nil || onHand != 14 {
		t.Fatalf("material on-hand = %v, err %v; want 14", onHand, err)
	}
	variant, err := finished.Get("variant-1")
	if err != nil {
		t.Fatalf("read finished variant: %v", err)
	}
	if variant.Stock != 6 {
		t.Fatalf("finished stock = %d, want initial 4 + 2 good units", variant.Stock)
	}
	if _, err := raw.Get("leather"); err != nil {
		t.Fatalf("raw material unexpectedly missing: %v", err)
	}
}

func TestManufacturingRejectsInvalidMutationsWithoutChangingState(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, *Service) error
		operation func(*Service) error
	}{
		{
			name: "BOM references a missing variant",
			operation: func(service *Service) error {
				_, err := service.SaveBOM(BOM{ID: "bad-bom", Revision: 1, VariantID: "missing", MaterialID: "leather", QuantityPerUnit: 1, Unit: "m2"}, "admin")
				return err
			},
		},
		{
			name: "BOM unit differs from material metadata",
			operation: func(service *Service) error {
				_, err := service.SaveBOM(BOM{ID: "bad-bom", Revision: 1, VariantID: "variant-1", MaterialID: "leather", QuantityPerUnit: 1, Unit: "kg"}, "admin")
				return err
			},
		},
		{
			name: "duplicate opening stock",
			prepare: func(t *testing.T, service *Service) error {
				_, err := service.OpenStock("leather", "m2", 10, "admin")
				return err
			},
			operation: func(service *Service) error {
				_, err := service.OpenStock("leather", "m2", 10, "admin")
				return err
			},
		},
		{
			name: "work order references a missing BOM",
			operation: func(service *Service) error {
				_, err := service.CreateWorkOrder("wo-1", "missing", 1, "planner")
				return err
			},
		},
		{
			name:    "work order has non-positive planned quantity",
			prepare: func(t *testing.T, service *Service) error { return seedBOMAndStock(service, 20) },
			operation: func(service *Service) error {
				_, err := service.CreateWorkOrder("wo-1", "bom-1", 0, "planner")
				return err
			},
		},
		{
			name: "draft work order cannot be completed",
			prepare: func(t *testing.T, service *Service) error {
				if err := seedBOMAndStock(service, 20); err != nil {
					return err
				}
				_, err := service.CreateWorkOrder("wo-1", "bom-1", 3, "planner")
				return err
			},
			operation: func(service *Service) error {
				_, err := service.CompleteWorkOrder("wo-1", 1, "operator")
				return err
			},
		},
		{
			name: "work order cannot be released twice",
			prepare: func(t *testing.T, service *Service) error {
				if err := seedReleasedWorkOrder(service, 20); err != nil {
					return err
				}
				return nil
			},
			operation: func(service *Service) error {
				_, err := service.ReleaseWorkOrder("wo-1", "supervisor")
				return err
			},
		},
		{
			name:    "good quantity must be positive",
			prepare: func(t *testing.T, service *Service) error { return seedReleasedWorkOrder(service, 20) },
			operation: func(service *Service) error {
				_, err := service.CompleteWorkOrder("wo-1", 0, "operator")
				return err
			},
		},
		{
			name:    "good quantity cannot exceed planned quantity",
			prepare: func(t *testing.T, service *Service) error { return seedReleasedWorkOrder(service, 20) },
			operation: func(service *Service) error {
				_, err := service.CompleteWorkOrder("wo-1", 4, "operator")
				return err
			},
		},
		{
			name:    "insufficient stock does not partially complete order",
			prepare: func(t *testing.T, service *Service) error { return seedReleasedWorkOrder(service, 5) },
			operation: func(service *Service) error {
				_, err := service.CompleteWorkOrder("wo-1", 2, "operator")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _, finished := newManufacturingFixture(t, 0)
			if test.prepare != nil {
				if err := test.prepare(t, service); err != nil {
					t.Fatalf("prepare case: %v", err)
				}
			}
			before := snapshotManufacturingState(t, service, finished)
			if err := test.operation(service); err == nil {
				t.Fatal("invalid mutation returned nil error")
			}
			after := snapshotManufacturingState(t, service, finished)
			if !reflect.DeepEqual(after, before) {
				t.Errorf("state changed after rejected mutation:\nbefore: %+v\nafter:  %+v", before, after)
			}
		})
	}
}

func TestCompletedWorkOrderCannotBeCompletedAgain(t *testing.T) {
	service, _, finished := newManufacturingFixture(t, 0)
	if err := seedReleasedWorkOrder(service, 20); err != nil {
		t.Fatalf("seed released work order: %v", err)
	}
	if _, err := service.CompleteWorkOrder("wo-1", 2, "operator"); err != nil {
		t.Fatalf("first completion: %v", err)
	}

	before := snapshotManufacturingState(t, service, finished)
	if _, err := service.CompleteWorkOrder("wo-1", 2, "operator"); err == nil {
		t.Fatal("duplicate completion succeeded")
	}
	after := snapshotManufacturingState(t, service, finished)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("state changed after duplicate completion:\nbefore: %+v\nafter:  %+v", before, after)
	}
}

type manufacturingState struct {
	BOMs      []BOM
	Orders    []WorkOrder
	Movements []MaterialMovement
	OnHand    int
	Stock     int
}

func snapshotManufacturingState(t *testing.T, service *Service, finished *finishedgoods.Service) manufacturingState {
	t.Helper()
	boms, err := service.ListBOMs()
	if err != nil {
		t.Fatalf("list BOMs: %v", err)
	}
	orders, err := service.ListWorkOrders()
	if err != nil {
		t.Fatalf("list work orders: %v", err)
	}
	movements, err := service.ListMaterialMovements()
	if err != nil {
		t.Fatalf("list movements: %v", err)
	}
	onHand, err := service.MaterialOnHand("leather")
	if err != nil {
		t.Fatalf("read on-hand: %v", err)
	}
	variant, err := finished.Get("variant-1")
	if err != nil {
		t.Fatalf("read finished variant: %v", err)
	}
	return manufacturingState{BOMs: boms, Orders: orders, Movements: movements, OnHand: onHand, Stock: variant.Stock}
}

func newManufacturingFixture(t *testing.T, initialStock int) (*Service, *rawmaterials.Service, *finishedgoods.Service) {
	t.Helper()
	raw := rawmaterials.New(store.NewMemoryRepository([]rawmaterials.RawMaterial{
		{ID: "leather", Name: "Leather", Unit: "m2"},
	}, func(item rawmaterials.RawMaterial) string { return item.ID }))
	finished := finishedgoods.New(store.NewMemoryRepository([]finishedgoods.FinishedProductVariant{
		{ID: "variant-1", Style: "Oxford", Size: "40", Color: "Black", Stock: initialStock, Dimensions: domain.Dimensions{LengthCM: 1, WidthCM: 2, HeightCM: 3, WeightKG: 0.5}},
	}, func(item finishedgoods.FinishedProductVariant) string { return item.ID }))
	return New(
		store.NewMemoryRepository([]BOM{}, func(item BOM) string { return item.ID }),
		store.NewMemoryRepository([]WorkOrder{}, func(item WorkOrder) string { return item.ID }),
		store.NewMemoryRepository([]MaterialMovement{}, func(item MaterialMovement) string { return item.ID }),
		raw,
		finished,
	), raw, finished
}

func seedBOMAndStock(service *Service, opening int) error {
	if _, err := service.SaveBOM(BOM{ID: "bom-1", Revision: 1, VariantID: "variant-1", MaterialID: "leather", QuantityPerUnit: 2, Unit: "m2"}, "admin"); err != nil {
		return err
	}
	_, err := service.OpenStock("leather", "m2", opening, "admin")
	return err
}

func seedReleasedWorkOrder(service *Service, opening int) error {
	if err := seedBOMAndStock(service, opening); err != nil {
		return err
	}
	if _, err := service.CreateWorkOrder("wo-1", "bom-1", 3, "planner"); err != nil {
		return err
	}
	_, err := service.ReleaseWorkOrder("wo-1", "supervisor")
	return err
}
