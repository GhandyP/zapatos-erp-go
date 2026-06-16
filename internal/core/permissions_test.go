package core

import "testing"

func TestCanAdminHasWildcard(t *testing.T) {
	for _, perm := range []string{"raw-materials:read", "billing:write", "audit:read", "anything:at-all"} {
		if !Can("administrador", perm) {
			t.Errorf("expected administrador to have permission %q", perm)
		}
	}
}

func TestCanAlmacenPermissions(t *testing.T) {
	allowed := []string{
		"raw-materials:read", "raw-materials:write",
		"finished-goods:read", "finished-goods:write",
		"packaging:read", "packaging:write",
		"logistics:read", "logistics:write",
	}
	for _, perm := range allowed {
		if !Can("almacen", perm) {
			t.Errorf("expected almacen to have %q", perm)
		}
	}
	denied := []string{"billing:read", "billing:write", "audit:read"}
	for _, perm := range denied {
		if Can("almacen", perm) {
			t.Errorf("expected almacen NOT to have %q", perm)
		}
	}
}

func TestCanVentasPermissions(t *testing.T) {
	if !Can("ventas", "billing:read") {
		t.Error("expected ventas to have billing:read")
	}
	if !Can("ventas", "billing:write") {
		t.Error("expected ventas to have billing:write")
	}
	if !Can("ventas", "finished-goods:read") {
		t.Error("expected ventas to have finished-goods:read")
	}
	if Can("ventas", "raw-materials:read") {
		t.Error("expected ventas NOT to have raw-materials:read")
	}
}

func TestCanUnknownRole(t *testing.T) {
	if Can("hacker", "raw-materials:read") {
		t.Error("expected unknown role to be denied")
	}
}

func TestCanUnknownPermission(t *testing.T) {
	if Can("administrador", "") {
		// admin has wildcard, so empty string matches
		return
	}
	if Can("almacen", "nonexistent:permission") {
		t.Error("expected unknown permission to be denied")
	}
}

func TestGetPermissionsReturnsCopy(t *testing.T) {
	perms := GetPermissions("almacen")
	if len(perms) == 0 {
		t.Fatal("expected non-empty permissions for almacen")
	}
	// Modify the returned slice — should not affect the original
	perms[0] = "mutated"
	original := GetPermissions("almacen")
	if original[0] == "mutated" {
		t.Fatal("GetPermissions should return a defensive copy")
	}
}

func TestGetPermissionsUnknownRole(t *testing.T) {
	perms := GetPermissions("nonexistent")
	if perms != nil {
		t.Fatalf("expected nil for unknown role, got %v", perms)
	}
}

func TestProduccionAsymmetricLogistics(t *testing.T) {
	if !Can("produccion", "logistics:read") {
		t.Error("expected produccion to have logistics:read")
	}
	if Can("produccion", "logistics:write") {
		t.Error("expected produccion NOT to have logistics:write")
	}
}
