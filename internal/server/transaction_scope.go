package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"zapatos-erp-go/internal/audit"
	"zapatos-erp-go/internal/modules/billing"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/modules/rawmaterials"
)

// TransactionServices contains the module services and audit writer bound to a
// single transaction. A TransactionScope must only expose services valid for
// the lifetime of the Run callback.
type TransactionServices struct {
	RawMaterials  *rawmaterials.Service
	FinishedGoods *finishedgoods.Service
	Packaging     *packaging.Service
	Logistics     *logistics.Service
	Billing       *billing.Service
	RecordAudit   func(actor, action, entity string) error
}

// TransactionScope runs a complete mutation callback atomically and returns
// only after the transaction has committed.
type TransactionScope interface {
	Run(ctx context.Context, operation func(TransactionServices) error) error
}

// transitionalLegacyJSONTransactionScope adapts the current JSON-backed services
// and audit store until app startup is migrated to a transactional backend.
type transitionalLegacyJSONTransactionScope struct {
	modules    *Modules
	auditStore *audit.Store
}

func (scope *transitionalLegacyJSONTransactionScope) Run(_ context.Context, operation func(TransactionServices) error) error {
	return operation(TransactionServices{
		RawMaterials:  scope.modules.RawMaterials,
		FinishedGoods: scope.modules.FinishedGoods,
		Packaging:     scope.modules.Packaging,
		Logistics:     scope.modules.Logistics,
		Billing:       scope.modules.Billing,
		RecordAudit: func(actor, action, entity string) error {
			scope.auditStore.Record(actor, action, entity)
			return nil
		},
	})
}

type serviceMutationError struct {
	err error
}

func (err *serviceMutationError) Error() string { return err.err.Error() }
func (err *serviceMutationError) Unwrap() error { return err.err }

func runMutation(ctx context.Context, scope TransactionScope, operation func(TransactionServices) error) error {
	if scope == nil {
		return errors.New("transaction scope is required")
	}

	called := false
	var operationErr error
	var callbackViolation error
	runErr := scope.Run(ctx, func(services TransactionServices) error {
		if called {
			callbackViolation = errors.New("transaction scope invoked operation more than once")
			return callbackViolation
		}
		called = true
		operationErr = operation(services)
		return operationErr
	})
	if callbackViolation != nil {
		return callbackViolation
	}
	if !called {
		if runErr != nil {
			return runErr
		}
		return errors.New("transaction scope did not invoke operation")
	}
	if operationErr != nil {
		var serviceErr *serviceMutationError
		if errors.As(operationErr, &serviceErr) {
			return operationErr
		}
		if runErr != nil {
			return runErr
		}
		return operationErr
	}
	return runErr
}

func mutationServiceError(err error) error {
	return &serviceMutationError{err: err}
}

func recordMutationAudit(services TransactionServices, actor, action, entity string) error {
	if services.RecordAudit == nil {
		return fmt.Errorf("transaction scope has no audit recorder")
	}
	return services.RecordAudit(actor, action, entity)
}

func respondMutationError(w http.ResponseWriter, err error) {
	var serviceErr *serviceMutationError
	if errors.As(err, &serviceErr) {
		respondServiceError(w, serviceErr.err)
		return
	}
	respondError(w, http.StatusInternalServerError, err)
}
