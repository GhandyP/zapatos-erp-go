package billing

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
	"time"

	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/store"
)

type InvoiceStatus string

const (
	StatusDraft  InvoiceStatus = "draft"
	StatusIssued InvoiceStatus = "issued"
	StatusPaid   InvoiceStatus = "paid"
	StatusVoid   InvoiceStatus = "void"
)

type Invoice struct {
	ID         string        `json:"id"`
	CustomerID string        `json:"customerId"`
	Total      float64       `json:"total"`
	Tax        float64       `json:"tax"`
	Status     InvoiceStatus `json:"status"`
}

type Service struct{ repo store.Repository[Invoice] }

func New(repo store.Repository[Invoice]) *Service { return &Service{repo: repo} }

func (s *Service) List() ([]Invoice, error) {
	items, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = normalize(items[i])
	}
	return items, nil
}

func (s *Service) Get(id string) (Invoice, error) {
	item, ok, err := s.repo.FindByID(id)
	if err != nil {
		return Invoice{}, err
	}
	if !ok {
		return Invoice{}, domain.NotFoundError{Entity: "Invoice", ID: id}
	}
	return normalize(item), nil
}

func (s *Service) Upsert(invoice Invoice) (Invoice, error) {
	if strings.TrimSpace(invoice.CustomerID) == "" {
		return Invoice{}, domain.ValidationError{Message: "Invoice requires customerId"}
	}
	if invoice.Total < 0 || invoice.Tax < 0 {
		return Invoice{}, domain.ValidationError{Message: "Invoice amounts cannot be negative"}
	}
	if strings.TrimSpace(invoice.ID) == "" {
		invoice.ID = createInvoiceID(invoice.CustomerID)
	}
	if invoice.Status == "" {
		invoice.Status = StatusDraft
	}
	if !isStatus(invoice.Status) {
		return Invoice{}, domain.ValidationError{Message: "Invalid invoice status"}
	}
	return s.repo.Save(normalize(invoice))
}

func (s *Service) Issue(id string) (Invoice, error) {
	invoice, err := s.Get(id)
	if err != nil {
		return Invoice{}, err
	}
	if invoice.Status != StatusDraft {
		return Invoice{}, domain.ValidationError{Message: "Only draft invoices can be issued"}
	}
	invoice.Status = StatusIssued
	return s.repo.Save(invoice)
}

func normalize(invoice Invoice) Invoice {
	if !isStatus(invoice.Status) {
		invoice.Status = StatusDraft
	}
	return invoice
}

func isStatus(value InvoiceStatus) bool {
	switch value {
	case StatusDraft, StatusIssued, StatusPaid, StatusVoid:
		return true
	default:
		return false
	}
}

var invoiceSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func createInvoiceID(customerID string) string {
	slug := strings.ToLower(strings.TrimSpace(customerID))
	slug = invoiceSlugRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "cliente"
	}
	return fmt.Sprintf("inv-%s-%s-%s", slug, time.Now().UTC().Format("20060102T150405Z"), randomToken(5))
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return fmt.Sprintf("%x", b)[:n]
}
