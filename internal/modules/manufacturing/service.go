package manufacturing

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/rawmaterials"
	"zapatos-erp-go/internal/store"
)

const (
	WorkOrderDraft     = "draft"
	WorkOrderReleased  = "released"
	WorkOrderCompleted = "completed"

	MovementOpeningStock = "opening_stock"
	MovementConsumption  = "consumption"
)

type BOM struct {
	ID              string  `json:"id"`
	Revision        int     `json:"revision"`
	VariantID       string  `json:"variantId"`
	MaterialID      string  `json:"materialId"`
	QuantityPerUnit float64 `json:"quantityPerUnit"`
	Unit            string  `json:"unit"`
	CreatedBy       string  `json:"createdBy"`
	CreatedAt       string  `json:"createdAt"`
}

type WorkOrder struct {
	ID              string  `json:"id"`
	BOMID           string  `json:"bomId"`
	BOMRevision     int     `json:"bomRevision"`
	VariantID       string  `json:"variantId"`
	MaterialID      string  `json:"materialId"`
	QuantityPerUnit float64 `json:"quantityPerUnit"`
	Unit            string  `json:"unit"`
	PlannedQty      int     `json:"plannedQty"`
	GoodQty         int     `json:"goodQty"`
	Status          string  `json:"status"`
	CreatedBy       string  `json:"createdBy"`
	CreatedAt       string  `json:"createdAt"`
	ReleasedBy      string  `json:"releasedBy,omitempty"`
	ReleasedAt      string  `json:"releasedAt,omitempty"`
	CompletedBy     string  `json:"completedBy,omitempty"`
	CompletedAt     string  `json:"completedAt,omitempty"`
}

type MaterialMovement struct {
	ID          string  `json:"id"`
	MaterialID  string  `json:"materialId"`
	Unit        string  `json:"unit"`
	Quantity    float64 `json:"quantity"`
	Kind        string  `json:"kind"`
	WorkOrderID string  `json:"workOrderId,omitempty"`
	Actor       string  `json:"actor"`
	At          string  `json:"at"`
}

type Service struct {
	boms          store.Repository[BOM]
	workOrders    store.Repository[WorkOrder]
	movements     store.Repository[MaterialMovement]
	rawMaterials  *rawmaterials.Service
	finishedGoods *finishedgoods.Service
}

// New binds manufacturing records to the repositories and transaction-bound
// inventory services supplied by the caller. Mutations spanning records must be
// invoked inside the caller's transaction scope.
func New(boms store.Repository[BOM], workOrders store.Repository[WorkOrder], movements store.Repository[MaterialMovement], rawMaterials *rawmaterials.Service, finishedGoods *finishedgoods.Service) *Service {
	return &Service{boms: boms, workOrders: workOrders, movements: movements, rawMaterials: rawMaterials, finishedGoods: finishedGoods}
}

func (s *Service) SaveBOM(bom BOM, actor string) (BOM, error) {
	if err := s.validateDependencies(); err != nil {
		return BOM{}, err
	}
	bom.ID = strings.TrimSpace(bom.ID)
	bom.VariantID = strings.TrimSpace(bom.VariantID)
	bom.MaterialID = strings.TrimSpace(bom.MaterialID)
	actor = strings.TrimSpace(actor)
	if bom.ID == "" || bom.VariantID == "" || bom.MaterialID == "" || bom.Revision < 1 {
		return BOM{}, domain.ValidationError{Message: "BOM requires id, positive revision, variant and material"}
	}
	if !positiveFinite(bom.QuantityPerUnit) {
		return BOM{}, domain.ValidationError{Message: "BOM quantityPerUnit must be positive and finite"}
	}
	if actor == "" {
		return BOM{}, domain.ValidationError{Message: "BOM actor is required"}
	}
	if _, err := s.finishedGoods.Get(bom.VariantID); err != nil {
		return BOM{}, err
	}
	material, err := s.rawMaterials.Get(bom.MaterialID)
	if err != nil {
		return BOM{}, err
	}
	if !matchingUnit(bom.Unit, material.Unit) {
		return BOM{}, domain.ValidationError{Message: "BOM unit must match raw material unit"}
	}
	bom.Unit = strings.TrimSpace(material.Unit)
	current, exists, err := s.boms.FindByID(bom.ID)
	if err != nil {
		return BOM{}, err
	}
	if (!exists && bom.Revision != 1) || (exists && bom.Revision != current.Revision+1) {
		return BOM{}, domain.ValidationError{Message: "BOM revision must start at 1 and increase by one"}
	}
	bom.CreatedBy = actor
	bom.CreatedAt = timestamp()
	return s.boms.Save(bom)
}

func (s *Service) GetBOM(id string) (BOM, error) {
	if err := s.validateDependencies(); err != nil {
		return BOM{}, err
	}
	bom, ok, err := s.boms.FindByID(strings.TrimSpace(id))
	if err != nil {
		return BOM{}, err
	}
	if !ok {
		return BOM{}, domain.NotFoundError{Entity: "BOM", ID: id}
	}
	return bom, nil
}

func (s *Service) ListBOMs() ([]BOM, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	return s.boms.List()
}

// OpenStock records the sole opening baseline for a material. Subsequent stock
// is derived from this movement and order-linked consumption movements.
func (s *Service) OpenStock(materialID, unit string, quantity float64, actor string) (MaterialMovement, error) {
	if err := s.validateDependencies(); err != nil {
		return MaterialMovement{}, err
	}
	materialID = strings.TrimSpace(materialID)
	actor = strings.TrimSpace(actor)
	if materialID == "" || actor == "" || !positiveFinite(quantity) {
		return MaterialMovement{}, domain.ValidationError{Message: "opening stock requires material, actor and positive finite quantity"}
	}
	material, err := s.rawMaterials.Get(materialID)
	if err != nil {
		return MaterialMovement{}, err
	}
	if !matchingUnit(unit, material.Unit) {
		return MaterialMovement{}, domain.ValidationError{Message: "opening stock unit must match raw material unit"}
	}
	movements, err := s.movements.List()
	if err != nil {
		return MaterialMovement{}, err
	}
	for _, movement := range movements {
		if movement.MaterialID == materialID && movement.Kind == MovementOpeningStock {
			return MaterialMovement{}, domain.ValidationError{Message: "opening stock already exists for material"}
		}
	}
	id := "opening:" + materialID
	if _, exists, err := s.movements.FindByID(id); err != nil {
		return MaterialMovement{}, err
	} else if exists {
		return MaterialMovement{}, domain.ValidationError{Message: "opening stock already exists for material"}
	}
	movement := MaterialMovement{
		ID: id, MaterialID: materialID, Unit: strings.TrimSpace(material.Unit),
		Quantity: quantity, Kind: MovementOpeningStock, Actor: actor, At: timestamp(),
	}
	return s.movements.Save(movement)
}

func (s *Service) ListMaterialMovements() ([]MaterialMovement, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	return s.movements.List()
}

func (s *Service) MaterialOnHand(materialID string) (float64, error) {
	if err := s.validateDependencies(); err != nil {
		return 0, err
	}
	materialID = strings.TrimSpace(materialID)
	if materialID == "" {
		return 0, domain.ValidationError{Message: "material id is required"}
	}
	material, err := s.rawMaterials.Get(materialID)
	if err != nil {
		return 0, err
	}
	movements, err := s.movements.List()
	if err != nil {
		return 0, err
	}
	var onHand float64
	for _, movement := range movements {
		if movement.MaterialID != materialID {
			continue
		}
		if !matchingUnit(movement.Unit, material.Unit) || !positiveFinite(movement.Quantity) {
			return 0, domain.ValidationError{Message: "material movement has invalid unit or quantity"}
		}
		switch movement.Kind {
		case MovementOpeningStock:
			onHand += movement.Quantity
		case MovementConsumption:
			onHand -= movement.Quantity
		default:
			return 0, domain.ValidationError{Message: "material movement has invalid kind"}
		}
		if math.IsInf(onHand, 0) || math.IsNaN(onHand) {
			return 0, domain.ValidationError{Message: "material on-hand quantity is not finite"}
		}
	}
	if onHand < 0 {
		return 0, domain.ValidationError{Message: "material on-hand quantity cannot be negative"}
	}
	return onHand, nil
}

func (s *Service) CreateWorkOrder(id, bomID string, plannedQty int, actor string) (WorkOrder, error) {
	if err := s.validateDependencies(); err != nil {
		return WorkOrder{}, err
	}
	id = strings.TrimSpace(id)
	bomID = strings.TrimSpace(bomID)
	actor = strings.TrimSpace(actor)
	if id == "" || bomID == "" || actor == "" || plannedQty <= 0 {
		return WorkOrder{}, domain.ValidationError{Message: "work order requires id, BOM, actor and positive planned quantity"}
	}
	if _, exists, err := s.workOrders.FindByID(id); err != nil {
		return WorkOrder{}, err
	} else if exists {
		return WorkOrder{}, domain.ValidationError{Message: "work order already exists"}
	}
	bom, err := s.GetBOM(bomID)
	if err != nil {
		return WorkOrder{}, err
	}
	if _, err := s.finishedGoods.Get(bom.VariantID); err != nil {
		return WorkOrder{}, err
	}
	material, err := s.rawMaterials.Get(bom.MaterialID)
	if err != nil {
		return WorkOrder{}, err
	}
	if !matchingUnit(bom.Unit, material.Unit) || !positiveFinite(bom.QuantityPerUnit) {
		return WorkOrder{}, domain.ValidationError{Message: "work order BOM is no longer valid for material metadata"}
	}
	order := WorkOrder{
		ID: id, BOMID: bom.ID, BOMRevision: bom.Revision, VariantID: bom.VariantID,
		MaterialID: bom.MaterialID, QuantityPerUnit: bom.QuantityPerUnit, Unit: bom.Unit,
		PlannedQty: plannedQty, Status: WorkOrderDraft, CreatedBy: actor, CreatedAt: timestamp(),
	}
	return s.workOrders.Save(order)
}

func (s *Service) GetWorkOrder(id string) (WorkOrder, error) {
	if err := s.validateDependencies(); err != nil {
		return WorkOrder{}, err
	}
	order, ok, err := s.workOrders.FindByID(strings.TrimSpace(id))
	if err != nil {
		return WorkOrder{}, err
	}
	if !ok {
		return WorkOrder{}, domain.NotFoundError{Entity: "WorkOrder", ID: id}
	}
	return order, nil
}

func (s *Service) ListWorkOrders() ([]WorkOrder, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	return s.workOrders.List()
}

func (s *Service) ReleaseWorkOrder(id, actor string) (WorkOrder, error) {
	if err := s.validateDependencies(); err != nil {
		return WorkOrder{}, err
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return WorkOrder{}, domain.ValidationError{Message: "work order actor is required"}
	}
	order, err := s.GetWorkOrder(id)
	if err != nil {
		return WorkOrder{}, err
	}
	if order.Status != WorkOrderDraft {
		return WorkOrder{}, domain.ValidationError{Message: "only draft work orders can be released"}
	}
	order.Status = WorkOrderReleased
	order.ReleasedBy = actor
	order.ReleasedAt = timestamp()
	return s.workOrders.Save(order)
}

func (s *Service) CompleteWorkOrder(id string, goodQty int, actor string) (WorkOrder, error) {
	if err := s.validateDependencies(); err != nil {
		return WorkOrder{}, err
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return WorkOrder{}, domain.ValidationError{Message: "work order actor is required"}
	}
	order, err := s.GetWorkOrder(id)
	if err != nil {
		return WorkOrder{}, err
	}
	if order.Status != WorkOrderReleased {
		return WorkOrder{}, domain.ValidationError{Message: "only released work orders can be completed"}
	}
	if goodQty <= 0 || goodQty > order.PlannedQty {
		return WorkOrder{}, domain.ValidationError{Message: "good quantity must be positive and cannot exceed planned quantity"}
	}
	material, err := s.rawMaterials.Get(order.MaterialID)
	if err != nil {
		return WorkOrder{}, err
	}
	if !matchingUnit(order.Unit, material.Unit) || !positiveFinite(order.QuantityPerUnit) {
		return WorkOrder{}, domain.ValidationError{Message: "work order material unit or BOM quantity is invalid"}
	}
	if _, err := s.finishedGoods.Get(order.VariantID); err != nil {
		return WorkOrder{}, err
	}
	required := float64(order.PlannedQty) * order.QuantityPerUnit
	if !positiveFinite(required) {
		return WorkOrder{}, domain.ValidationError{Message: "planned material consumption must be positive and finite"}
	}
	onHand, err := s.MaterialOnHand(order.MaterialID)
	if err != nil {
		return WorkOrder{}, err
	}
	if onHand < required {
		return WorkOrder{}, domain.ValidationError{Message: fmt.Sprintf("insufficient material stock: need %g %s, have %g", required, order.Unit, onHand)}
	}
	if _, exists, err := s.movements.FindByID("consumption:" + order.ID); err != nil {
		return WorkOrder{}, err
	} else if exists {
		return WorkOrder{}, domain.ValidationError{Message: "work order consumption already exists"}
	}
	variant, err := s.finishedGoods.Get(order.VariantID)
	if err != nil {
		return WorkOrder{}, err
	}
	maxInt := int(^uint(0) >> 1)
	if variant.Stock > maxInt-goodQty {
		return WorkOrder{}, domain.ValidationError{Message: "finished stock would overflow"}
	}

	movement := MaterialMovement{
		ID: "consumption:" + order.ID, MaterialID: order.MaterialID, Unit: strings.TrimSpace(material.Unit),
		Quantity: required, Kind: MovementConsumption, WorkOrderID: order.ID, Actor: actor, At: timestamp(),
	}
	if _, err := s.movements.Save(movement); err != nil {
		return WorkOrder{}, err
	}
	if _, err := s.finishedGoods.AdjustStock(order.VariantID, goodQty, "manufacturing order "+order.ID); err != nil {
		return WorkOrder{}, err
	}
	order.Status = WorkOrderCompleted
	order.GoodQty = goodQty
	order.CompletedBy = actor
	order.CompletedAt = timestamp()
	return s.workOrders.Save(order)
}

func (s *Service) validateDependencies() error {
	if s == nil || s.boms == nil || s.workOrders == nil || s.movements == nil || s.rawMaterials == nil || s.finishedGoods == nil {
		return errors.New("manufacturing service dependencies are required")
	}
	return nil
}

func matchingUnit(value, expected string) bool {
	return strings.TrimSpace(value) != "" && strings.TrimSpace(value) == strings.TrimSpace(expected)
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
