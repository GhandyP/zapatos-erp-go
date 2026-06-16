package foxpro

import (
	"sync"

	"zapatos-erp-go/internal/modules/billing"
)

type SyncResult struct {
	LastSyncAt    *string `json:"lastSyncAt"`
	Imported      int     `json:"imported"`
	Updated       int     `json:"updated"`
	Failed        int     `json:"failed"`
	PendingIssued int     `json:"pendingIssued"`
	SyncedIssued  int     `json:"syncedIssued"`
}

type BillingService interface {
	List() ([]billing.Invoice, error)
}

type Adapter struct {
	mu              sync.RWMutex
	billing         BillingService
	lastSyncAt      *string
	lastImported    int
	lastUpdated     int
	lastFailed      int
	syncedInvoiceID map[string]struct{}
}

func New(billing BillingService) *Adapter {
	return &Adapter{billing: billing, syncedInvoiceID: map[string]struct{}{}}
}

func buildStatus(svc BillingService, syncedInvoiceID map[string]struct{}, lastSyncAt *string, imported, updated, failed int) (SyncResult, error) {
	invoices, err := svc.List()
	if err != nil {
		return SyncResult{}, err
	}
	issued := 0
	pending := 0
	for _, invoice := range invoices {
		if invoice.Status != billing.StatusIssued {
			continue
		}
		issued++
		if _, ok := syncedInvoiceID[invoice.ID]; !ok {
			pending++
		}
	}
	return SyncResult{LastSyncAt: lastSyncAt, Imported: imported, Updated: updated, Failed: failed, PendingIssued: pending, SyncedIssued: len(syncedInvoiceID)}, nil
}

func (a *Adapter) GetStatus() (SyncResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return buildStatus(a.billing, a.syncedInvoiceID, a.lastSyncAt, a.lastImported, a.lastUpdated, a.lastFailed)
}

func (a *Adapter) Sync() (SyncResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	invoices, err := a.billing.List()
	if err != nil {
		return SyncResult{}, err
	}
	pending := make([]billing.Invoice, 0)
	issued := 0
	for _, invoice := range invoices {
		if invoice.Status != billing.StatusIssued {
			continue
		}
		issued++
		if _, ok := a.syncedInvoiceID[invoice.ID]; !ok {
			pending = append(pending, invoice)
		}
	}
	for _, invoice := range pending {
		a.syncedInvoiceID[invoice.ID] = struct{}{}
	}
	now := timeNow()
	a.lastSyncAt = &now
	a.lastImported = len(pending)
	a.lastUpdated = issued - len(pending)
	a.lastFailed = 0
	return buildStatus(a.billing, a.syncedInvoiceID, a.lastSyncAt, a.lastImported, a.lastUpdated, 0)
}

func timeNow() string { return billingTimeNow() }
