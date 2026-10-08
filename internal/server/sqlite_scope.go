package server

import (
	"context"
	"database/sql"
	"errors"

	"zapatos-erp-go/internal/audit"
	"zapatos-erp-go/internal/modules/billing"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/manufacturing"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/modules/rawmaterials"
	"zapatos-erp-go/internal/store"
)

// NewSQLiteTransactionScope creates a production transaction scope whose
// module repositories and audit writer share the same SQLite transaction.
func NewSQLiteTransactionScope(db *sql.DB) TransactionScope {
	return &sqliteTransactionScope{unitOfWork: store.NewSQLiteUnitOfWork(db)}
}

type sqliteTransactionScope struct {
	unitOfWork *store.SQLiteUnitOfWork
}

func (scope *sqliteTransactionScope) Run(ctx context.Context, operation func(TransactionServices) error) error {
	if scope == nil || scope.unitOfWork == nil {
		return errors.New("SQLite transaction scope has no unit of work")
	}
	if operation == nil {
		return errors.New("SQLite transaction scope has no operation")
	}

	return scope.unitOfWork.Run(ctx, func(tx *sql.Tx) error {
		rawMaterialService := rawmaterials.New(store.NewSQLiteRepository(tx, store.CollectionRawMaterials,
			func(item rawmaterials.RawMaterial) string { return item.ID }))
		finishedGoodsService := finishedgoods.New(store.NewSQLiteRepository(tx, store.CollectionFinishedGoods,
			func(item finishedgoods.FinishedProductVariant) string { return item.ID }))
		services := TransactionServices{
			RawMaterials:  rawMaterialService,
			FinishedGoods: finishedGoodsService,
			Manufacturing: manufacturing.New(
				store.NewSQLiteRepository(tx, store.CollectionManufacturingBOMs, func(item manufacturing.BOM) string { return item.ID }),
				store.NewSQLiteRepository(tx, store.CollectionManufacturingWorkOrders, func(item manufacturing.WorkOrder) string { return item.ID }),
				store.NewSQLiteRepository(tx, store.CollectionMaterialMovements, func(item manufacturing.MaterialMovement) string { return item.ID }),
				rawMaterialService,
				finishedGoodsService,
			),
			Packaging: packaging.New(store.NewSQLiteRepository(tx, store.CollectionPackaging,
				func(item packaging.PackagingSpec) string { return item.ID })),
			Logistics: logistics.New(store.NewSQLiteRepository(tx, store.CollectionLogistics,
				func(item logistics.StorageSlot) string { return item.ID })),
			Billing: billing.New(store.NewSQLiteRepository(tx, store.CollectionInvoices,
				func(item billing.Invoice) string { return item.ID })),
		}
		writer := audit.NewSQLiteWriter(tx, store.CollectionAuditEvents)
		services.RecordAudit = func(actor, action, entity string) error {
			_, err := writer.Record(actor, action, entity)
			return err
		}
		return operation(services)
	})
}
