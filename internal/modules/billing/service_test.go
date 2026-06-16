package billing

import (
	"testing"

	"zapatos-erp-go/internal/store"
)

func newTestService() *Service {
	return New(store.NewMemoryRepository([]Invoice{}, func(v Invoice) string { return v.ID }))
}

func TestUpsertRequiresCustomerID(t *testing.T) {
	svc := newTestService()
	_, err := svc.Upsert(Invoice{Total: 100})
	if err == nil {
		t.Fatal("expected validation error for missing customerId")
	}
}

func TestUpsertRejectsNegativeAmounts(t *testing.T) {
	svc := newTestService()
	_, err := svc.Upsert(Invoice{CustomerID: "cli-1", Total: -10})
	if err == nil {
		t.Fatal("expected validation error for negative total")
	}
	_, err = svc.Upsert(Invoice{CustomerID: "cli-1", Total: 100, Tax: -5})
	if err == nil {
		t.Fatal("expected validation error for negative tax")
	}
}

func TestUpsertGeneratesIDWhenEmpty(t *testing.T) {
	svc := newTestService()
	item, err := svc.Upsert(Invoice{CustomerID: "cli-1", Total: 100, Tax: 21})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.ID == "" {
		t.Fatal("expected generated ID")
	}
	if item.Status != StatusDraft {
		t.Fatalf("expected default status draft, got %s", item.Status)
	}
}

func TestUpsertAcceptsValidStatus(t *testing.T) {
	svc := newTestService()
	for _, status := range []InvoiceStatus{StatusDraft, StatusIssued, StatusPaid, StatusVoid} {
		item, err := svc.Upsert(Invoice{ID: "inv-" + string(status), CustomerID: "cli-1", Total: 100, Status: status})
		if err != nil {
			t.Fatalf("unexpected error for status %s: %v", status, err)
		}
		if item.Status != status {
			t.Fatalf("expected status %s, got %s", status, item.Status)
		}
	}
}

func TestUpsertRejectsInvalidStatus(t *testing.T) {
	svc := newTestService()
	_, err := svc.Upsert(Invoice{CustomerID: "cli-1", Total: 100, Status: "bogus"})
	if err == nil {
		t.Fatal("expected validation error for invalid status")
	}
}

func TestIssueDraftInvoice(t *testing.T) {
	svc := newTestService()
	saved, _ := svc.Upsert(Invoice{ID: "inv-1", CustomerID: "cli-1", Total: 100, Status: StatusDraft})
	issued, err := svc.Issue(saved.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued.Status != StatusIssued {
		t.Fatalf("expected issued status, got %s", issued.Status)
	}
}

func TestIssueRejectsNonDraft(t *testing.T) {
	svc := newTestService()
	for _, status := range []InvoiceStatus{StatusIssued, StatusPaid, StatusVoid} {
		svc.Upsert(Invoice{ID: "inv-" + string(status), CustomerID: "cli-1", Total: 100, Status: status})
		_, err := svc.Issue("inv-" + string(status))
		if err == nil {
			t.Fatalf("expected error issuing %s invoice", status)
		}
	}
}

func TestIssueNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.Issue("nonexistent")
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestGetNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.Get("nonexistent")
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestListReturnsNormalized(t *testing.T) {
	svc := newTestService()
	svc.Upsert(Invoice{ID: "inv-1", CustomerID: "cli-1", Total: 100})
	items, err := svc.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Status != StatusDraft {
		t.Fatalf("expected draft status, got %s", items[0].Status)
	}
}

func TestRandomTokenUniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok := randomToken(8)
		if tokens[tok] {
			t.Fatalf("collision on token %q at iteration %d", tok, i)
		}
		tokens[tok] = true
	}
}
