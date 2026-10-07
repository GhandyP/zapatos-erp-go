package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"zapatos-erp-go/internal/audit"
	"zapatos-erp-go/internal/auth"
	"zapatos-erp-go/internal/integrations/foxpro"
	"zapatos-erp-go/internal/modules/billing"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/modules/rawmaterials"
	"zapatos-erp-go/internal/store"
	"zapatos-erp-go/internal/web"
)

func TestHandlerDimensionsFlows(t *testing.T) {
	root := t.TempDir()
	sessions := auth.NewSessionStore(filepath.Join(root, "sessions.json"), []auth.UserAccount{
		auth.NewUserAccount("admin", "admin123", "administrador"),
	})
	auditStore := audit.NewStore(filepath.Join(root, "audit.json"))
	h := NewHandler(&Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
	}, sessions, auditStore, web.NewUI())

	token := loginAs(t, h, "admin", "admin123")

	resp := performRequest(t, h, http.MethodPost, "/api/packaging", map[string]any{
		"id":            "pkg-1",
		"name":          "Caja master",
		"transportMode": "camion",
		"maxUnits":      10,
		"dimensions":    map[string]any{"lengthCm": 30, "widthCm": 20, "heightCm": 10, "weightKg": 1},
	}, token)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = performRequest(t, h, http.MethodGet, "/api/packaging", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte("pkg-1")) {
		t.Fatalf("expected response to contain saved item, got %s", resp.Body.String())
	}

	resp = performRequest(t, h, http.MethodPost, "/api/logistics", map[string]any{
		"id":            "slot-1",
		"name":          "Rack A",
		"zone":          "Z1",
		"capacityUnits": 8,
		"dimensions":    map[string]any{"lengthCm": 10, "widthCm": 12, "heightCm": 14, "weightKg": 1},
	}, token)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = performRequest(t, h, http.MethodPost, "/api/finished-goods", map[string]any{
		"style":      "Oxford",
		"size":       "40",
		"color":      "Negro",
		"stock":      1,
		"dimensions": map[string]any{"lengthCm": 28, "widthCm": 10, "heightCm": 12, "weightKg": 0.8},
	}, token)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected finished goods 201, got %d: %s", resp.Code, resp.Body.String())
	}
	var createdFinished finishedgoods.FinishedProductVariant
	if err := json.Unmarshal(resp.Body.Bytes(), &createdFinished); err != nil {
		t.Fatalf("decode created finished good: %v", err)
	}

	resp = performRequest(t, h, http.MethodPost, "/api/raw-materials", map[string]any{
		"id":         "rm-1",
		"name":       "Cuero",
		"unit":       "m2",
		"minStock":   2,
		"dimensions": map[string]any{"lengthCm": 5, "widthCm": 5, "heightCm": 1, "weightKg": 0.2},
	}, token)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected raw materials 201, got %d: %s", resp.Code, resp.Body.String())
	}

	wantAuditEntities := map[string]bool{
		"packaging:pkg-1":                      false,
		"logistics:slot-1":                     false,
		"finished-goods:" + createdFinished.ID: false,
		"raw-materials:rm-1":                   false,
	}
	for _, event := range auditStore.List() {
		if event.Actor != "admin" || event.Action != "saved" {
			t.Fatalf("legacy audit event = %#v, want admin saved event", event)
		}
		if _, ok := wantAuditEntities[event.Entity]; !ok {
			t.Errorf("unexpected legacy audit entity %q", event.Entity)
			continue
		}
		wantAuditEntities[event.Entity] = true
	}
	if len(auditStore.List()) != len(wantAuditEntities) {
		t.Fatalf("legacy audit events = %d, want %d for raw materials, finished goods, packaging, and logistics", len(auditStore.List()), len(wantAuditEntities))
	}
	for entity, recorded := range wantAuditEntities {
		if !recorded {
			t.Errorf("legacy constructor did not audit %q", entity)
		}
	}

	resp = performRequest(t, h, http.MethodGet, "/app", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected app 200, got %d", resp.Code)
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte("Embalaje / transporte")) || !bytes.Contains(resp.Body.Bytes(), []byte("Stock y logística")) {
		t.Fatalf("expected app html to include new areas")
	}
}

func TestHandlerAppliesSecurityHeaders(t *testing.T) {
	h := NewHandler(&Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
	}, auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []auth.UserAccount{
		auth.NewUserAccount("admin", "admin123", "administrador"),
	}), audit.NewStore(filepath.Join(t.TempDir(), "audit.json")), web.NewUI())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("Referrer-Policy = %q, want same-origin", got)
	}
}

func TestHardeningRecoversFromPanic(t *testing.T) {
	h := withHardening(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal server error") {
		t.Fatalf("expected recovery response, got %s", rec.Body.String())
	}
}

func TestObservabilityEchoesTraceHeadersAndServesMetrics(t *testing.T) {
	h := observabilityTestHandler(t)

	trace := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-Id", "req-123")
	req.Header.Set("Traceparent", trace)
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "req-123" {
		t.Fatalf("X-Request-Id = %q, want req-123", got)
	}
	if got := rec.Header().Get("Traceparent"); got != trace {
		t.Fatalf("Traceparent = %q, want %q", got, trace)
	}

	metrics := httptest.NewRecorder()
	h.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK {
		t.Fatalf("expected metrics 200, got %d: %s", metrics.Code, metrics.Body.String())
	}
	var snapshot map[string]any
	if err := json.Unmarshal(metrics.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if _, ok := snapshot["requests_total"]; !ok {
		t.Fatalf("expected requests_total in metrics snapshot, got %#v", snapshot)
	}
	if _, ok := snapshot["status_counts"]; !ok {
		t.Fatalf("expected status_counts in metrics snapshot, got %#v", snapshot)
	}
}

func TestRespondErrorRedactsInternalFailures(t *testing.T) {
	rec := httptest.NewRecorder()
	respondError(rec, http.StatusInternalServerError, fmt.Errorf("secret token leaked"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret token leaked") {
		t.Fatalf("expected redacted body, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal server error") {
		t.Fatalf("expected generic internal error body, got %s", rec.Body.String())
	}
}

func TestDecodeJSONBodyRejectsOversizedPayload(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Username string `json:"username"`
		}
		if err := decodeJSONBody(w, r, 8, &payload); err != nil {
			if isBodyTooLarge(err) {
				respondError(w, http.StatusRequestEntityTooLarge, err)
				return
			}
			respondError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"`+strings.Repeat("a", 64)+`","password":"x"}`))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too large") && !strings.Contains(rec.Body.String(), "request body") {
		t.Fatalf("expected body-too-large response, got %s", rec.Body.String())
	}
}

func TestNewHTTPServerUsesConservativeTimeouts(t *testing.T) {
	server := newHTTPServer(":8080", http.NewServeMux())

	if server.ReadHeaderTimeout <= 0 {
		t.Fatal("expected ReadHeaderTimeout to be set")
	}
	if server.ReadTimeout <= 0 {
		t.Fatal("expected ReadTimeout to be set")
	}
	if server.WriteTimeout <= 0 {
		t.Fatal("expected WriteTimeout to be set")
	}
	if server.IdleTimeout <= 0 {
		t.Fatal("expected IdleTimeout to be set")
	}
}

func TestHandlerReloadsPersistentSessionAndAuditOrder(t *testing.T) {
	root := t.TempDir()
	sessionsPath := filepath.Join(root, "sessions.json")
	auditPath := filepath.Join(root, "audit.json")
	writeAuditEvents(t, auditPath, []audit.Event{
		{Actor: "audit", Action: "later", Entity: "packaging:pkg-2", At: "2026-06-11T10:00:00Z"},
		{Actor: "audit", Action: "earlier", Entity: "packaging:pkg-1", At: "2026-06-11T09:00:00Z"},
	})

	first := NewHandler(&Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
	}, auth.NewSessionStore(sessionsPath, []auth.UserAccount{
		auth.NewUserAccount("admin", "admin123", "administrador"),
		auth.NewUserAccount("audit", "audit123", "auditoria"),
	}), audit.NewStore(auditPath), web.NewUI())
	adminToken := loginAs(t, first, "admin", "admin123")
	if adminToken == "" {
		t.Fatal("expected admin token")
	}

	restarted := NewHandler(&Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
	}, auth.NewSessionStore(sessionsPath, []auth.UserAccount{
		auth.NewUserAccount("admin", "admin123", "administrador"),
		auth.NewUserAccount("audit", "audit123", "auditoria"),
	}), audit.NewStore(auditPath), web.NewUI())

	reloadResp := performRequest(t, restarted, http.MethodGet, "/api/me", nil, adminToken)
	if reloadResp.Code != http.StatusOK {
		t.Fatalf("expected reloaded session to authorize, got %d: %s", reloadResp.Code, reloadResp.Body.String())
	}

	auditToken := loginAs(t, restarted, "audit", "audit123")
	resp := performRequest(t, restarted, http.MethodGet, "/api/audit", nil, auditToken)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected audit 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var events []audit.Event
	if err := json.Unmarshal(resp.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	want := []audit.Event{
		{Actor: "audit", Action: "earlier", Entity: "packaging:pkg-1", At: "2026-06-11T09:00:00Z"},
		{Actor: "audit", Action: "later", Entity: "packaging:pkg-2", At: "2026-06-11T10:00:00Z"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestHTTPContractCoreAPIResourcesDocumentCurrentRoutes(t *testing.T) {
	contract, err := os.ReadFile("../../docs/http-contract.md")
	if err != nil {
		t.Fatalf("read HTTP contract: %v", err)
	}

	var section []string
	inSection := false
	foundSection := false
	for _, line := range strings.Split(string(contract), "\n") {
		if line == "## Core API resources" {
			foundSection = true
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}
		if inSection {
			section = append(section, line)
		}
	}
	if !foundSection {
		t.Fatal("HTTP contract is missing the ## Core API resources section")
	}
	sectionText := strings.Join(section, "\n")

	for _, route := range []string{
		"GET /api/invoices",
		"POST /api/invoices",
		"GET /api/invoices/{id}",
		"POST /api/invoices/{id}/issue",
		"GET /api/foxpro",
		"POST /api/foxpro/sync",
	} {
		if !strings.Contains(sectionText, "`"+route+"`") {
			t.Errorf("Core API resources section does not document %q", route)
		}
	}

	for _, obsoleteRoute := range []string{
		"/api/billing",
		"/api/foxpro/status",
		"PATCH /api/invoices",
	} {
		if strings.Contains(sectionText, obsoleteRoute) {
			t.Errorf("Core API resources section still documents obsolete route %q", obsoleteRoute)
		}
	}
}

func TestHandlerInvoiceAndFoxProRuntimeContract(t *testing.T) {
	h, auditStore := httpContractTestHandler(t)
	token := loginAs(t, h, "admin", "admin123")

	resp := performRequest(t, h, http.MethodGet, "/api/invoices", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/invoices = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("GET /api/invoices Content-Type = %q", got)
	}
	var invoices []billing.Invoice
	if err := json.Unmarshal(resp.Body.Bytes(), &invoices); err != nil {
		t.Fatalf("decode invoice list: %v", err)
	}
	if len(invoices) != 0 {
		t.Fatalf("initial invoice list has %d entries, want 0", len(invoices))
	}

	resp = performRequest(t, h, http.MethodPost, "/api/invoices", map[string]any{
		"id": "inv-contract", "customerId": "customer-1", "total": 100, "tax": 21, "status": "draft",
	}, token)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST /api/invoices = %d, want 201: %s", resp.Code, resp.Body.String())
	}
	var created billing.Invoice
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created invoice: %v", err)
	}
	if created.ID != "inv-contract" || created.CustomerID != "customer-1" || created.Total != 100 || created.Tax != 21 || created.Status != billing.StatusDraft {
		t.Fatalf("created invoice = %#v, want stable invoice fields", created)
	}

	resp = performRequest(t, h, http.MethodGet, "/api/invoices", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/invoices after create = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &invoices); err != nil {
		t.Fatalf("decode populated invoice list: %v", err)
	}
	if len(invoices) != 1 || invoices[0].ID != "inv-contract" {
		t.Fatalf("invoice list = %#v, want the saved invoice", invoices)
	}

	resp = performRequest(t, h, http.MethodGet, "/api/invoices/inv-contract", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/invoices/{id} = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	var fetched billing.Invoice
	if err := json.Unmarshal(resp.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("decode fetched invoice: %v", err)
	}
	if fetched.ID != "inv-contract" || fetched.CustomerID != "customer-1" || fetched.Status != billing.StatusDraft {
		t.Fatalf("fetched invoice = %#v, want saved invoice fields", fetched)
	}

	resp = performRequest(t, h, http.MethodPost, "/api/invoices/inv-contract/issue", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST /api/invoices/{id}/issue = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	var issued billing.Invoice
	if err := json.Unmarshal(resp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issued invoice: %v", err)
	}
	if issued.ID != "inv-contract" || issued.Status != billing.StatusIssued {
		t.Fatalf("issued invoice = %#v, want same ID with issued status", issued)
	}
	invoiceAudit := auditStore.List()
	if len(invoiceAudit) != 2 || invoiceAudit[0].Actor != "admin" || invoiceAudit[0].Action != "saved" || invoiceAudit[0].Entity != "billing:inv-contract" || invoiceAudit[1].Actor != "admin" || invoiceAudit[1].Action != "issued" || invoiceAudit[1].Entity != "billing:inv-contract" {
		t.Fatalf("legacy invoice audit events = %#v, want saved then issued for billing:inv-contract", invoiceAudit)
	}

	resp = performRequest(t, h, http.MethodGet, "/api/invoices/missing", nil, token)
	assertContractError(t, resp, http.StatusNotFound, "Invoice not found: missing")
	resp = performRequest(t, h, http.MethodPost, "/api/invoices", map[string]any{"customerId": "", "total": 1, "tax": 0}, token)
	assertContractError(t, resp, http.StatusBadRequest, "Invoice requires customerId")

	resp = performRequest(t, h, http.MethodGet, "/api/foxpro", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/foxpro = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	var status foxpro.SyncResult
	assertContractJSONFields(t, resp, "lastSyncAt", "imported", "updated", "failed", "pendingIssued", "syncedIssued")
	if err := json.Unmarshal(resp.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode FoxPro status: %v", err)
	}
	if status.LastSyncAt != nil || status.Imported != 0 || status.Updated != 0 || status.Failed != 0 || status.PendingIssued != 1 || status.SyncedIssued != 0 {
		t.Fatalf("initial FoxPro result = %#v, want one pending issued invoice and zero sync counts", status)
	}

	resp = performRequest(t, h, http.MethodPost, "/api/foxpro/sync", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST /api/foxpro/sync = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	assertContractJSONFields(t, resp, "lastSyncAt", "imported", "updated", "failed", "pendingIssued", "syncedIssued")
	if err := json.Unmarshal(resp.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode FoxPro sync result: %v", err)
	}
	if status.LastSyncAt == nil || *status.LastSyncAt == "" || status.Imported != 1 || status.Updated != 0 || status.Failed != 0 || status.PendingIssued != 0 || status.SyncedIssued != 1 {
		t.Fatalf("FoxPro sync result = %#v, want non-empty timestamp and stable counts", status)
	}
}

func TestHandlerWithTransactionScopeAcceptsSQLiteSessionStore(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "sessions.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	h := NewHandlerWithTransactionScope(
		transactionTestModules(),
		auth.NewSQLiteSessionStore(db, []auth.UserAccount{auth.NewUserAccount("admin", "admin123", "administrador")}),
		audit.NewStore(filepath.Join(t.TempDir(), "audit.json")),
		web.NewUI(),
		newRecordingTransactionScope(),
	)
	token := loginAs(t, h, "admin", "admin123")
	resp := performRequest(t, h, http.MethodGet, "/api/me", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/me with SQLite session = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	var got auth.SessionUser
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode /api/me session: %v", err)
	}
	if want := (auth.SessionUser{Username: "admin", Role: "administrador"}); got != want {
		t.Fatalf("GET /api/me session = %+v, want %+v", got, want)
	}
}

func TestAuditRoutesUseContextAwareSQLiteAccess(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	auditAccess := audit.NewSQLiteStore(db)
	if _, err := auditAccess.RecordContext(context.Background(), "admin", "saved", "packaging:pkg-sqlite"); err != nil {
		t.Fatalf("seed SQLite audit event: %v", err)
	}

	h := NewHandlerWithTransactionScope(
		transactionTestModules(),
		auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []auth.UserAccount{auth.NewUserAccount("admin", "admin123", "administrador")}),
		auditAccess,
		web.NewUI(),
		NewSQLiteTransactionScope(db),
	)
	token := loginAs(t, h, "admin", "admin123")

	resp := performRequest(t, h, http.MethodGet, "/api/audit", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/audit = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	var events []audit.Event
	if err := json.Unmarshal(resp.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	if len(events) != 1 || events[0].Entity != "packaging:pkg-sqlite" {
		t.Fatalf("GET /api/audit events = %#v, want seeded SQLite event", events)
	}

	resp = performRequest(t, h, http.MethodGet, "/api/audit/export", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/audit/export = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("audit export Content-Type = %q, want CSV", got)
	}
	if !strings.Contains(resp.Body.String(), "at,actor,action,entity,module") || !strings.Contains(resp.Body.String(), "packaging:pkg-sqlite,packaging") {
		t.Fatalf("audit export does not contain seeded SQLite event: %s", resp.Body.String())
	}
}

func TestAuditReadRoutesSurfaceSQLiteErrors(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	h := NewHandlerWithTransactionScope(
		transactionTestModules(),
		auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []auth.UserAccount{auth.NewUserAccount("admin", "admin123", "administrador")}),
		audit.NewSQLiteStore(db),
		web.NewUI(),
		nil,
	)
	token := loginAs(t, h, "admin", "admin123")
	if err := db.Close(); err != nil {
		t.Fatalf("close SQLite database: %v", err)
	}

	for _, path := range []string{"/api/audit", "/api/audit/export"} {
		t.Run(path, func(t *testing.T) {
			resp := performRequest(t, h, http.MethodGet, path, nil, token)
			if resp.Code != http.StatusInternalServerError {
				t.Fatalf("GET %s = %d, want 500: %s", path, resp.Code, resp.Body.String())
			}
			if !strings.Contains(resp.Body.String(), "internal server error") {
				t.Fatalf("GET %s error response = %s, want generic internal error", path, resp.Body.String())
			}
		})
	}
}

func TestFoxProSyncRecordsThroughStandaloneSQLiteAuditAccess(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	invoices := billing.New(store.NewMemoryRepository([]billing.Invoice{
		{ID: "inv-issued", CustomerID: "customer-1", Total: 100, Tax: 21, Status: billing.StatusIssued},
	}, func(item billing.Invoice) string { return item.ID }))
	modules := transactionTestModules()
	modules.Billing = invoices
	modules.Foxpro = foxpro.New(invoices)
	h := NewHandlerWithTransactionScope(
		modules,
		auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []auth.UserAccount{auth.NewUserAccount("admin", "admin123", "administrador")}),
		audit.NewSQLiteStore(db),
		web.NewUI(),
		NewSQLiteTransactionScope(db),
	)
	token := loginAs(t, h, "admin", "admin123")
	resp := performRequest(t, h, http.MethodPost, "/api/foxpro/sync", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST /api/foxpro/sync = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	events, err := audit.NewSQLiteStore(db).ListContext(context.Background())
	if err != nil {
		t.Fatalf("list SQLite audit events: %v", err)
	}
	if len(events) != 1 || events[0].Actor != "admin" || events[0].Action != "synced" || events[0].Entity != "foxpro:issued-invoices" {
		t.Fatalf("FoxPro sync audit events = %#v, want standalone sync event", events)
	}
}

func TestHandlerAuthStatusDistinction(t *testing.T) {
	h, _ := httpContractTestHandler(t)
	for _, test := range []struct {
		name  string
		token string
	}{
		{name: "missing token"},
		{name: "invalid token", token: "not-a-session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			me := performRequest(t, h, http.MethodGet, "/api/me", nil, test.token)
			assertContractError(t, me, http.StatusUnauthorized, "unauthorized")

			resource := performRequest(t, h, http.MethodGet, "/api/invoices", nil, test.token)
			assertContractError(t, resource, http.StatusForbidden, "forbidden")
		})
	}
}

func httpContractTestHandler(t *testing.T) (http.Handler, *audit.Store) {
	t.Helper()
	root := t.TempDir()
	invoices := billing.New(store.NewMemoryRepository([]billing.Invoice{}, func(v billing.Invoice) string { return v.ID }))
	auditStore := audit.NewStore(filepath.Join(root, "audit.json"))
	handler := NewHandler(&Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
		Billing:       invoices,
		Foxpro:        foxpro.New(invoices),
	}, auth.NewSessionStore(filepath.Join(root, "sessions.json"), []auth.UserAccount{
		auth.NewUserAccount("admin", "admin123", "administrador"),
	}), auditStore, web.NewUI())
	return handler, auditStore
}

func assertContractJSONFields(t *testing.T, resp *httptest.ResponseRecorder, fields ...string) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON response fields: %v", err)
	}
	for _, field := range fields {
		if _, ok := payload[field]; !ok {
			t.Errorf("JSON response is missing field %q: %s", field, resp.Body.String())
		}
	}
}

func assertContractError(t *testing.T, resp *httptest.ResponseRecorder, wantStatus int, wantError string) {
	t.Helper()
	if resp.Code != wantStatus {
		t.Fatalf("status = %d, want %d: %s", resp.Code, wantStatus, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("error Content-Type = %q, want application/json; charset=utf-8", got)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error != wantError {
		t.Fatalf("error = %q, want %q", payload.Error, wantError)
	}
}

func writeAuditEvents(t *testing.T, path string, events []audit.Event) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir audit: %v", err)
	}
	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		t.Fatalf("marshal audit events: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write audit events: %v", err)
	}
}

func performRequest(t *testing.T, h http.Handler, method, path string, body map[string]any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func loginAs(t *testing.T, h http.Handler, username, password string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return resp.Token
}

func rawSvc() *rawmaterials.Service {
	return rawmaterials.New(store.NewMemoryRepository([]rawmaterials.RawMaterial{}, func(v rawmaterials.RawMaterial) string { return v.ID }))
}

func finishedSvc() *finishedgoods.Service {
	return finishedgoods.New(store.NewMemoryRepository([]finishedgoods.FinishedProductVariant{}, func(v finishedgoods.FinishedProductVariant) string { return v.ID }))
}

func packagingSvc() *packaging.Service {
	return packaging.New(store.NewMemoryRepository([]packaging.PackagingSpec{}, func(v packaging.PackagingSpec) string { return v.ID }))
}

func logisticsSvc() *logistics.Service {
	return logistics.New(store.NewMemoryRepository([]logistics.StorageSlot{}, func(v logistics.StorageSlot) string { return v.ID }))
}

func observabilityTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return NewHandler(&Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
	}, auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), []auth.UserAccount{
		auth.NewUserAccount("admin", "admin123", "administrador"),
	}), audit.NewStore(filepath.Join(t.TempDir(), "audit.json")), web.NewUI())
}

type transactionScopeTestContextKey struct{}

type transactionAuditRecord struct {
	actor  string
	action string
	entity string
}

type recordingTransactionScope struct {
	services     TransactionServices
	runErr       error
	auditErr     error
	runCount     int
	auditRecords []transactionAuditRecord
	contextValue any
}

func newRecordingTransactionScope() *recordingTransactionScope {
	scope := &recordingTransactionScope{}
	scope.services = TransactionServices{
		RawMaterials: rawmaterials.New(store.NewMemoryRepository([]rawmaterials.RawMaterial{
			{ID: "rm-existing", Name: "Leather", Unit: "m2"},
		}, func(item rawmaterials.RawMaterial) string { return item.ID })),
		FinishedGoods: finishedgoods.New(store.NewMemoryRepository([]finishedgoods.FinishedProductVariant{
			{ID: "fg-existing", Style: "Oxford", Size: "40", Color: "Negro", Stock: 2},
		}, func(item finishedgoods.FinishedProductVariant) string { return item.ID })),
		Packaging: packaging.New(store.NewMemoryRepository([]packaging.PackagingSpec{
			{ID: "pkg-existing", Name: "Caja", TransportMode: "camion", MaxUnits: 2},
		}, func(item packaging.PackagingSpec) string { return item.ID })),
		Logistics: logistics.New(store.NewMemoryRepository([]logistics.StorageSlot{
			{ID: "slot-existing", Name: "Rack A", Zone: "Z1", CapacityUnits: 2},
		}, func(item logistics.StorageSlot) string { return item.ID })),
		Billing: billing.New(store.NewMemoryRepository([]billing.Invoice{
			{ID: "inv-existing", CustomerID: "customer-1", Total: 100, Tax: 21, Status: billing.StatusDraft},
		}, func(item billing.Invoice) string { return item.ID })),
	}
	scope.services.RecordAudit = func(actor, action, entity string) error {
		scope.auditRecords = append(scope.auditRecords, transactionAuditRecord{actor: actor, action: action, entity: entity})
		return scope.auditErr
	}
	return scope
}

func (scope *recordingTransactionScope) Run(ctx context.Context, operation func(TransactionServices) error) error {
	scope.runCount++
	scope.contextValue = ctx.Value(transactionScopeTestContextKey{})
	if err := operation(scope.services); err != nil {
		return err
	}
	return scope.runErr
}

func transactionTestModules() *Modules {
	return &Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
		Billing:       billing.New(store.NewMemoryRepository([]billing.Invoice{}, func(item billing.Invoice) string { return item.ID })),
	}
}

func transactionTestHandler(t *testing.T, modules *Modules, scope TransactionScope) http.Handler {
	t.Helper()
	root := t.TempDir()
	return NewHandlerWithTransactionScope(modules, auth.NewSessionStore(filepath.Join(root, "sessions.json"), []auth.UserAccount{
		auth.NewUserAccount("admin", "admin123", "administrador"),
	}), audit.NewStore(filepath.Join(root, "audit.json")), web.NewUI(), scope)
}

type repeatedCallbackTransactionScope struct{}

func (repeatedCallbackTransactionScope) Run(_ context.Context, operation func(TransactionServices) error) error {
	_ = operation(TransactionServices{})
	_ = operation(TransactionServices{})
	return nil
}

func TestRunMutationRejectsRepeatedCallback(t *testing.T) {
	operationCalls := 0
	err := runMutation(context.Background(), repeatedCallbackTransactionScope{}, func(TransactionServices) error {
		operationCalls++
		return nil
	})
	if err == nil {
		t.Fatal("runMutation succeeded after the scope invoked its callback twice")
	}
	if operationCalls != 1 {
		t.Fatalf("mutation operation ran %d times, want once", operationCalls)
	}
}

func TestTransactionalHandlerRunsAllPersistedMutationsInsideScope(t *testing.T) {
	dimensions := map[string]any{"lengthCm": 10, "widthCm": 12, "heightCm": 14, "weightKg": 1}
	tests := []struct {
		name   string
		method string
		path   string
		body   map[string]any
		status int
		action string
		entity string
		verify func(*testing.T, TransactionServices)
	}{
		{
			name: "raw material create", method: http.MethodPost, path: "/api/raw-materials",
			body:   map[string]any{"id": "rm-new", "name": "Leather", "unit": "m2", "minStock": 1, "dimensions": dimensions},
			status: http.StatusCreated, action: "saved", entity: "raw-materials:rm-new",
			verify: func(t *testing.T, services TransactionServices) {
				if _, err := services.RawMaterials.Get("rm-new"); err != nil {
					t.Fatalf("transaction-bound raw material was not saved: %v", err)
				}
			},
		},
		{
			name: "raw material delete", method: http.MethodDelete, path: "/api/raw-materials/rm-existing",
			status: http.StatusNoContent, action: "deleted", entity: "raw-materials:rm-existing",
			verify: func(t *testing.T, services TransactionServices) {
				if _, err := services.RawMaterials.Get("rm-existing"); err == nil {
					t.Fatal("transaction-bound raw material was not deleted")
				}
			},
		},
		{
			name: "finished good create", method: http.MethodPost, path: "/api/finished-goods",
			body:   map[string]any{"id": "fg-new", "style": "Oxford", "size": "40", "color": "Negro", "stock": 1, "dimensions": dimensions},
			status: http.StatusCreated, action: "saved", entity: "finished-goods:fg-new",
			verify: func(t *testing.T, services TransactionServices) {
				if _, err := services.FinishedGoods.Get("fg-new"); err != nil {
					t.Fatalf("transaction-bound finished good was not saved: %v", err)
				}
			},
		},
		{
			name: "finished good stock adjustment", method: http.MethodPatch, path: "/api/finished-goods/fg-existing",
			body:   map[string]any{"delta": 1, "note": "count"},
			status: http.StatusOK, action: "adjusted stock", entity: "finished-goods:fg-existing",
			verify: func(t *testing.T, services TransactionServices) {
				item, err := services.FinishedGoods.Get("fg-existing")
				if err != nil || item.Stock != 3 {
					t.Fatalf("transaction-bound stock = %d, err %v; want 3, nil", item.Stock, err)
				}
			},
		},
		{
			name: "packaging create", method: http.MethodPost, path: "/api/packaging",
			body:   map[string]any{"id": "pkg-new", "name": "Caja", "transportMode": "camion", "maxUnits": 10, "dimensions": dimensions},
			status: http.StatusCreated, action: "saved", entity: "packaging:pkg-new",
			verify: func(t *testing.T, services TransactionServices) {
				if _, err := services.Packaging.Get("pkg-new"); err != nil {
					t.Fatalf("transaction-bound packaging was not saved: %v", err)
				}
			},
		},
		{
			name: "packaging delete", method: http.MethodDelete, path: "/api/packaging/pkg-existing",
			status: http.StatusNoContent, action: "deleted", entity: "packaging:pkg-existing",
			verify: func(t *testing.T, services TransactionServices) {
				if _, err := services.Packaging.Get("pkg-existing"); err == nil {
					t.Fatal("transaction-bound packaging was not deleted")
				}
			},
		},
		{
			name: "logistics create", method: http.MethodPost, path: "/api/logistics",
			body:   map[string]any{"id": "slot-new", "name": "Rack B", "zone": "Z2", "capacityUnits": 8, "dimensions": dimensions},
			status: http.StatusCreated, action: "saved", entity: "logistics:slot-new",
			verify: func(t *testing.T, services TransactionServices) {
				if _, err := services.Logistics.Get("slot-new"); err != nil {
					t.Fatalf("transaction-bound logistics slot was not saved: %v", err)
				}
			},
		},
		{
			name: "logistics delete", method: http.MethodDelete, path: "/api/logistics/slot-existing",
			status: http.StatusNoContent, action: "deleted", entity: "logistics:slot-existing",
			verify: func(t *testing.T, services TransactionServices) {
				if _, err := services.Logistics.Get("slot-existing"); err == nil {
					t.Fatal("transaction-bound logistics slot was not deleted")
				}
			},
		},
		{
			name: "invoice create", method: http.MethodPost, path: "/api/invoices",
			body:   map[string]any{"id": "inv-new", "customerId": "customer-1", "total": 100, "tax": 21, "status": "draft"},
			status: http.StatusCreated, action: "saved", entity: "billing:inv-new",
			verify: func(t *testing.T, services TransactionServices) {
				if _, err := services.Billing.Get("inv-new"); err != nil {
					t.Fatalf("transaction-bound invoice was not saved: %v", err)
				}
			},
		},
		{
			name: "invoice issue", method: http.MethodPost, path: "/api/invoices/inv-existing/issue",
			status: http.StatusOK, action: "issued", entity: "billing:inv-existing",
			verify: func(t *testing.T, services TransactionServices) {
				item, err := services.Billing.Get("inv-existing")
				if err != nil || item.Status != billing.StatusIssued {
					t.Fatalf("transaction-bound invoice status = %q, err %v; want issued, nil", item.Status, err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scope := newRecordingTransactionScope()
			h := transactionTestHandler(t, transactionTestModules(), scope)
			token := loginAs(t, h, "admin", "admin123")
			resp := performTransactionRequest(t, h, test.method, test.path, test.body, token)
			if resp.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", resp.Code, test.status, resp.Body.String())
			}
			if scope.runCount != 1 {
				t.Fatalf("transaction scope called %d times, want once", scope.runCount)
			}
			if scope.contextValue != "request-context" {
				t.Fatalf("scope context value = %#v, want request context", scope.contextValue)
			}
			if len(scope.auditRecords) != 1 {
				t.Fatalf("audit callbacks = %d, want one", len(scope.auditRecords))
			}
			if got := scope.auditRecords[0]; got != (transactionAuditRecord{actor: "admin", action: test.action, entity: test.entity}) {
				t.Fatalf("audit record = %#v, want actor admin, action %q, entity %q", got, test.action, test.entity)
			}
			test.verify(t, scope.services)
		})
	}
}

func TestTransactionalHandlerAuditsOnlyAfterServiceSuccess(t *testing.T) {
	scope := newRecordingTransactionScope()
	h := transactionTestHandler(t, transactionTestModules(), scope)
	token := loginAs(t, h, "admin", "admin123")
	resp := performTransactionRequest(t, h, http.MethodPost, "/api/raw-materials", map[string]any{
		"id": "", "name": "Leather", "unit": "m2", "minStock": 1,
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.Code, resp.Body.String())
	}
	if scope.runCount != 1 {
		t.Fatalf("transaction scope called %d times, want once", scope.runCount)
	}
	if len(scope.auditRecords) != 0 {
		t.Fatalf("audit callbacks = %d, want none after service failure", len(scope.auditRecords))
	}
	if !strings.Contains(resp.Body.String(), "Raw material requires id and name") {
		t.Fatalf("service validation response changed: %s", resp.Body.String())
	}
}

func TestTransactionalHandlerFailsClosedOnScopeAndAuditErrors(t *testing.T) {
	tests := []struct {
		name        string
		configure   func(*recordingTransactionScope)
		useNilScope bool
	}{
		{name: "scope run error", configure: func(scope *recordingTransactionScope) { scope.runErr = errors.New("commit failed") }},
		{name: "audit error", configure: func(scope *recordingTransactionScope) { scope.auditErr = errors.New("audit insert failed") }},
		{name: "missing scope", useNilScope: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			modules := transactionTestModules()
			var scope *recordingTransactionScope
			var injectedScope TransactionScope
			if !test.useNilScope {
				scope = newRecordingTransactionScope()
				test.configure(scope)
				injectedScope = scope
			}
			h := transactionTestHandler(t, modules, injectedScope)
			token := loginAs(t, h, "admin", "admin123")
			resp := performTransactionRequest(t, h, http.MethodPost, "/api/raw-materials", map[string]any{
				"id": "rm-new", "name": "Leather", "unit": "m2", "minStock": 1,
				"dimensions": map[string]any{"lengthCm": 1, "widthCm": 1, "heightCm": 1, "weightKg": 1},
			}, token)
			if resp.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500: %s", resp.Code, resp.Body.String())
			}
			if strings.Contains(resp.Body.String(), "commit failed") || strings.Contains(resp.Body.String(), "audit insert failed") {
				t.Fatalf("internal transaction error leaked: %s", resp.Body.String())
			}
			if _, err := modules.RawMaterials.Get("rm-new"); err == nil {
				t.Fatal("transactional failure fell back to the legacy module service")
			}
			if scope != nil && scope.runCount != 1 {
				t.Fatalf("transaction scope called %d times, want once", scope.runCount)
			}
		})
	}
}

func performTransactionRequest(t *testing.T, h http.Handler, method, path string, body map[string]any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req = req.WithContext(context.WithValue(req.Context(), transactionScopeTestContextKey{}, "request-context"))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
