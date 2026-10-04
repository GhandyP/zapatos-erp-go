package server

import (
	"bytes"
	"encoding/json"
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
	h := NewHandler(&Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
	}, sessions, audit.NewStore(filepath.Join(root, "audit.json")), web.NewUI())

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
	h := httpContractTestHandler(t)
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

func TestHandlerAuthStatusDistinction(t *testing.T) {
	h := httpContractTestHandler(t)
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

func httpContractTestHandler(t *testing.T) http.Handler {
	t.Helper()
	root := t.TempDir()
	invoices := billing.New(store.NewMemoryRepository([]billing.Invoice{}, func(v billing.Invoice) string { return v.ID }))
	return NewHandler(&Modules{
		RawMaterials:  rawSvc(),
		FinishedGoods: finishedSvc(),
		Packaging:     packagingSvc(),
		Logistics:     logisticsSvc(),
		Billing:       invoices,
		Foxpro:        foxpro.New(invoices),
	}, auth.NewSessionStore(filepath.Join(root, "sessions.json"), []auth.UserAccount{
		auth.NewUserAccount("admin", "admin123", "administrador"),
	}), audit.NewStore(filepath.Join(root, "audit.json")), web.NewUI())
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
