package core

var permissionsByRole = map[string][]string{
	"administrador": {"*"},
	"almacen":       {"raw-materials:read", "raw-materials:write", "finished-goods:read", "finished-goods:write", "packaging:read", "packaging:write", "logistics:read", "logistics:write"},
	"produccion":    {"raw-materials:read", "raw-materials:write", "finished-goods:read", "finished-goods:write", "packaging:read", "packaging:write", "logistics:read"},
	"ventas":        {"billing:read", "billing:write", "finished-goods:read"},
	"contabilidad":  {"billing:read", "billing:write"},
	"auditoria":     {"audit:read", "raw-materials:read", "finished-goods:read", "billing:read", "packaging:read", "logistics:read"},
}

func Can(role, permission string) bool {
	perms, ok := permissionsByRole[role]
	if !ok {
		return false
	}
	for _, perm := range perms {
		if perm == "*" || perm == permission {
			return true
		}
	}
	return false
}

func GetPermissions(role string) []string {
	perms, ok := permissionsByRole[role]
	if !ok {
		return nil
	}
	out := make([]string, len(perms))
	copy(out, perms)
	return out
}
