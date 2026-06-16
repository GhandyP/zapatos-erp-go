package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"zapatos-erp-go/internal/audit"
	"zapatos-erp-go/internal/auth"
	"zapatos-erp-go/internal/core"
	"zapatos-erp-go/internal/domain"
	"zapatos-erp-go/internal/integrations/foxpro"
	"zapatos-erp-go/internal/modules/billing"
	"zapatos-erp-go/internal/modules/finishedgoods"
	"zapatos-erp-go/internal/modules/logistics"
	"zapatos-erp-go/internal/modules/packaging"
	"zapatos-erp-go/internal/modules/rawmaterials"
	"zapatos-erp-go/internal/observability"
	"zapatos-erp-go/internal/web"
)

const (
	requestBodyLimit  int64 = 1 << 20
	readHeaderTimeout       = 5 * time.Second
	readTimeout             = 15 * time.Second
	writeTimeout            = 15 * time.Second
	idleTimeout             = 60 * time.Second
	shutdownTimeout         = 5 * time.Second
)

type Modules struct {
	RawMaterials  *rawmaterials.Service
	FinishedGoods *finishedgoods.Service
	Packaging     *packaging.Service
	Logistics     *logistics.Service
	Billing       *billing.Service
	Foxpro        *foxpro.Adapter
}

func Run(ctx context.Context, addr string, modules *Modules, sessions *auth.SessionStore, auditStore *audit.Store, ui *web.UI) error {
	server := newHTTPServer(addr, NewHandler(modules, sessions, auditStore, ui))

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Zapatos ERP HTTP server running on http://localhost%s\n", addr)
	return server.ListenAndServe()
}

func NewHandler(modules *Modules, sessions *auth.SessionStore, auditStore *audit.Store, ui *web.UI) http.Handler {
	obs := observability.New(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	mux := http.NewServeMux()
	mux.Handle("/metrics", obs.MetricsHandler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"ok": true, "app": "Zapatos ERP"})
	})
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		var payload struct{ Username, Password string }
		if err := decodeJSONBody(w, r, requestBodyLimit, &payload); err != nil {
			if isBodyTooLarge(err) {
				respondError(w, http.StatusRequestEntityTooLarge, err)
				return
			}
			respondError(w, http.StatusBadRequest, err)
			return
		}
		token, user, ok := sessions.Login(payload.Username, payload.Password)
		if !ok {
			respondJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid credentials"})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
	})
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, session, ok := currentSession(r, sessions)
		if !ok {
			respondJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		respondJSON(w, http.StatusOK, session)
	})
	mux.HandleFunc("/api/boot", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"roles": []string{"administrador", "almacen", "produccion", "ventas", "contabilidad", "auditoria"}})
	})
	mux.HandleFunc("/api/raw-materials", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method == http.MethodGet {
			if !core.Can(role, "raw-materials:read") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			items, err := modules.RawMaterials.List()
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			respondJSON(w, http.StatusOK, items)
			return
		}
		if r.Method == http.MethodPost {
			if !core.Can(role, "raw-materials:write") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			var item rawmaterials.RawMaterial
			if err := decodeJSONBody(w, r, requestBodyLimit, &item); err != nil {
				if isBodyTooLarge(err) {
					respondError(w, http.StatusRequestEntityTooLarge, err)
					return
				}
				respondError(w, http.StatusBadRequest, err)
				return
			}
			saved, err := modules.RawMaterials.Upsert(item)
			if err != nil {
				respondServiceError(w, err)
				return
			}
			auditStore.Record(actorName(session), "saved", "raw-materials:"+saved.ID)
			respondJSON(w, http.StatusCreated, saved)
			return
		}
		methodNotAllowed(w, "GET, POST")
	})
	mux.HandleFunc("/api/raw-materials/", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method != http.MethodDelete {
			methodNotAllowed(w, "DELETE")
			return
		}
		if !core.Can(role, "raw-materials:write") {
			respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/raw-materials/")
		if err := modules.RawMaterials.Remove(id); err != nil {
			respondServiceError(w, err)
			return
		}
		auditStore.Record(actorName(session), "deleted", "raw-materials:"+id)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/finished-goods", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method == http.MethodGet {
			if !core.Can(role, "finished-goods:read") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			items, err := modules.FinishedGoods.List()
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			respondJSON(w, http.StatusOK, items)
			return
		}
		if r.Method == http.MethodPost {
			if !core.Can(role, "finished-goods:write") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			var item finishedgoods.FinishedProductVariant
			if err := decodeJSONBody(w, r, requestBodyLimit, &item); err != nil {
				if isBodyTooLarge(err) {
					respondError(w, http.StatusRequestEntityTooLarge, err)
					return
				}
				respondError(w, http.StatusBadRequest, err)
				return
			}
			saved, err := modules.FinishedGoods.Upsert(item)
			if err != nil {
				respondServiceError(w, err)
				return
			}
			auditStore.Record(actorName(session), "saved", "finished-goods:"+saved.ID)
			respondJSON(w, http.StatusCreated, saved)
			return
		}
		methodNotAllowed(w, "GET, POST")
	})
	mux.HandleFunc("/api/finished-goods/", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		id := strings.TrimPrefix(r.URL.Path, "/api/finished-goods/")
		if r.Method == http.MethodGet {
			if !core.Can(role, "finished-goods:read") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			item, err := modules.FinishedGoods.Get(id)
			if err != nil {
				respondServiceError(w, err)
				return
			}
			respondJSON(w, http.StatusOK, item)
			return
		}
		if r.Method == http.MethodPatch {
			if !core.Can(role, "finished-goods:write") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			var payload struct {
				Delta int    `json:"delta"`
				Note  string `json:"note"`
			}
			if err := decodeJSONBody(w, r, requestBodyLimit, &payload); err != nil {
				if isBodyTooLarge(err) {
					respondError(w, http.StatusRequestEntityTooLarge, err)
					return
				}
				respondError(w, http.StatusBadRequest, err)
				return
			}
			item, err := modules.FinishedGoods.AdjustStock(id, payload.Delta, payload.Note)
			if err != nil {
				respondServiceError(w, err)
				return
			}
			auditStore.Record(actorName(session), "adjusted stock", "finished-goods:"+item.ID)
			respondJSON(w, http.StatusOK, item)
			return
		}
		notFound(w)
	})
	mux.HandleFunc("/api/packaging", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method == http.MethodGet {
			if !core.Can(role, "packaging:read") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			items, err := modules.Packaging.List()
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			respondJSON(w, http.StatusOK, items)
			return
		}
		if r.Method == http.MethodPost {
			if !core.Can(role, "packaging:write") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			var item packaging.PackagingSpec
			if err := decodeJSONBody(w, r, requestBodyLimit, &item); err != nil {
				if isBodyTooLarge(err) {
					respondError(w, http.StatusRequestEntityTooLarge, err)
					return
				}
				respondError(w, http.StatusBadRequest, err)
				return
			}
			saved, err := modules.Packaging.Upsert(item)
			if err != nil {
				respondServiceError(w, err)
				return
			}
			auditStore.Record(actorName(session), "saved", "packaging:"+saved.ID)
			respondJSON(w, http.StatusCreated, saved)
			return
		}
		methodNotAllowed(w, "GET, POST")
	})
	mux.HandleFunc("/api/packaging/", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method != http.MethodDelete {
			methodNotAllowed(w, "DELETE")
			return
		}
		if !core.Can(role, "packaging:write") {
			respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/packaging/")
		if err := modules.Packaging.Remove(id); err != nil {
			respondServiceError(w, err)
			return
		}
		auditStore.Record(actorName(session), "deleted", "packaging:"+id)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/logistics", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method == http.MethodGet {
			if !core.Can(role, "logistics:read") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			items, err := modules.Logistics.List()
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			respondJSON(w, http.StatusOK, items)
			return
		}
		if r.Method == http.MethodPost {
			if !core.Can(role, "logistics:write") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			var item logistics.StorageSlot
			if err := decodeJSONBody(w, r, requestBodyLimit, &item); err != nil {
				if isBodyTooLarge(err) {
					respondError(w, http.StatusRequestEntityTooLarge, err)
					return
				}
				respondError(w, http.StatusBadRequest, err)
				return
			}
			saved, err := modules.Logistics.Upsert(item)
			if err != nil {
				respondServiceError(w, err)
				return
			}
			auditStore.Record(actorName(session), "saved", "logistics:"+saved.ID)
			respondJSON(w, http.StatusCreated, saved)
			return
		}
		methodNotAllowed(w, "GET, POST")
	})
	mux.HandleFunc("/api/logistics/", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method != http.MethodDelete {
			methodNotAllowed(w, "DELETE")
			return
		}
		if !core.Can(role, "logistics:write") {
			respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/logistics/")
		if err := modules.Logistics.Remove(id); err != nil {
			respondServiceError(w, err)
			return
		}
		auditStore.Record(actorName(session), "deleted", "logistics:"+id)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/invoices", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method == http.MethodGet {
			if !core.Can(role, "billing:read") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			items, err := modules.Billing.List()
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			respondJSON(w, http.StatusOK, items)
			return
		}
		if r.Method == http.MethodPost {
			if !core.Can(role, "billing:write") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			var item billing.Invoice
			if err := decodeJSONBody(w, r, requestBodyLimit, &item); err != nil {
				if isBodyTooLarge(err) {
					respondError(w, http.StatusRequestEntityTooLarge, err)
					return
				}
				respondError(w, http.StatusBadRequest, err)
				return
			}
			saved, err := modules.Billing.Upsert(item)
			if err != nil {
				respondServiceError(w, err)
				return
			}
			auditStore.Record(actorName(session), "saved", "billing:"+saved.ID)
			respondJSON(w, http.StatusCreated, saved)
			return
		}
		methodNotAllowed(w, "GET, POST")
	})
	mux.HandleFunc("/api/invoices/", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		path := strings.TrimPrefix(r.URL.Path, "/api/invoices/")
		if strings.HasSuffix(path, "/issue") && r.Method == http.MethodPost {
			if !core.Can(role, "billing:write") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			id := strings.TrimSuffix(path, "/issue")
			id = strings.TrimSuffix(id, "/")
			item, err := modules.Billing.Issue(id)
			if err != nil {
				respondServiceError(w, err)
				return
			}
			auditStore.Record(actorName(session), "issued", "billing:"+item.ID)
			respondJSON(w, http.StatusOK, item)
			return
		}
		if r.Method == http.MethodGet {
			if !core.Can(role, "billing:read") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			item, err := modules.Billing.Get(strings.TrimSuffix(path, "/"))
			if err != nil {
				respondServiceError(w, err)
				return
			}
			respondJSON(w, http.StatusOK, item)
			return
		}
		methodNotAllowed(w, "GET, POST")
	})
	mux.HandleFunc("/api/foxpro", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method == http.MethodGet {
			if !core.Can(role, "foxpro:read") {
				respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			status, err := modules.Foxpro.GetStatus()
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			respondJSON(w, http.StatusOK, status)
			return
		}
		methodNotAllowed(w, "GET")
	})
	mux.HandleFunc("/api/foxpro/sync", func(w http.ResponseWriter, r *http.Request) {
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		if !core.Can(role, "foxpro:sync") {
			respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		status, err := modules.Foxpro.Sync()
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		auditStore.Record(actorName(session), "synced", "foxpro:issued-invoices")
		respondJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/api/audit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if !core.Can(role, "audit:read") {
			respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		respondJSON(w, http.StatusOK, filterAuditEvents(auditStore.List(), r))
	})
	mux.HandleFunc("/api/audit/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, session, _ := currentSession(r, sessions)
		role := session.Role
		if !core.Can(role, "audit:read") {
			respondJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		events := filterAuditEvents(auditStore.List(), r)
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"audit-%s.csv\"", time.Now().UTC().Format("2006-01-02")))
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"at", "actor", "action", "entity", "module"})
		for _, event := range events {
			module := strings.SplitN(event.Entity, ":", 2)[0]
			_ = writer.Write([]string{event.At, event.Actor, event.Action, event.Entity, module})
		}
		writer.Flush()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/app" {
			notFound(w)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, ui.Render())
	})
	mux.HandleFunc("/app", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app" {
			notFound(w)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, ui.Render())
	})
	return obs.Wrap(withHardening(mux))
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func withHardening(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		defer func() {
			if recovered := recover(); recovered != nil {
				respondError(w, http.StatusInternalServerError, fmt.Errorf("internal server error: %v", recovered))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func setSecurityHeaders(w http.ResponseWriter) {
	headers := w.Header()
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")
	headers.Set("Referrer-Policy", "same-origin")
	headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}

func currentSession(r *http.Request, sessions *auth.SessionStore) (string, auth.SessionUser, bool) {
	token := auth.GetBearerToken(r.Header.Get("Authorization"))
	if user, ok := sessions.Get(token); ok {
		return token, user, true
	}
	return "", auth.SessionUser{}, false
}

func actorName(session auth.SessionUser) string {
	if session.Username != "" {
		return session.Username
	}
	if session.Role != "" {
		return session.Role
	}
	return "system"
}

func filterAuditEvents(events []audit.Event, r *http.Request) []audit.Event {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	actor := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("actor")))
	entity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entity")))
	action := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("action")))
	from := parseTime(r.URL.Query().Get("from"))
	to := parseTime(r.URL.Query().Get("to"))
	out := make([]audit.Event, 0, len(events))
	for _, event := range events {
		eventTime, _ := time.Parse(time.RFC3339Nano, event.At)
		haystack := strings.ToLower(strings.Join([]string{event.Actor, event.Action, event.Entity, event.At}, " "))
		module := strings.ToLower(strings.SplitN(event.Entity, ":", 2)[0])
		if !from.IsZero() && eventTime.Before(from) {
			continue
		}
		if !to.IsZero() && eventTime.After(to.Add(24*time.Hour-time.Nanosecond)) {
			continue
		}
		if q != "" && !strings.Contains(haystack, q) {
			continue
		}
		if actor != "" && !strings.Contains(strings.ToLower(event.Actor), actor) {
			continue
		}
		if action != "" && !strings.Contains(strings.ToLower(event.Action), action) {
			continue
		}
		if entity != "" && !strings.Contains(strings.ToLower(event.Entity), entity) && !strings.Contains(module, entity) {
			continue
		}
		out = append(out, event)
	}
	return out
}

func parseTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, value)
	if !t.IsZero() {
		return t
	}
	t, _ = time.Parse("2006-01-02", value)
	return t
}

func respondJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func respondError(w http.ResponseWriter, status int, err error) {
	respondJSON(w, status, map[string]any{"error": errorMessageForStatus(status, err)})
}

func respondServiceError(w http.ResponseWriter, err error) {
	var validation domain.ValidationError
	var notFound domain.NotFoundError
	switch {
	case errors.As(err, &validation):
		respondJSON(w, http.StatusBadRequest, map[string]any{"error": validation.Error()})
	case errors.As(err, &notFound):
		respondJSON(w, http.StatusNotFound, map[string]any{"error": notFound.Error()})
	default:
		respondJSON(w, http.StatusInternalServerError, map[string]any{"error": errorMessageForStatus(http.StatusInternalServerError, err)})
	}
}

func errorMessageForStatus(status int, err error) string {
	if status >= http.StatusInternalServerError {
		return "internal server error"
	}
	return err.Error()
}

func decodeJSON(body io.Reader, target any) error {
	dec := json.NewDecoder(body)
	return dec.Decode(target)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) error {
	return decodeJSON(limitedBody(w, r, maxBytes), target)
}

func isBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr) || strings.Contains(strings.ToLower(err.Error()), "too large")
}

func limitedBody(w http.ResponseWriter, r *http.Request, maxBytes int64) io.ReadCloser {
	return http.MaxBytesReader(w, r.Body, maxBytes)
}

func notFound(w http.ResponseWriter) {
	respondJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	w.WriteHeader(http.StatusMethodNotAllowed)
}
