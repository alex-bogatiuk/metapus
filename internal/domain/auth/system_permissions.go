package auth

// Permissions of the platform's system administration. The routes they guard
// are hand-written, so the entity registry cannot derive them; SystemPermissions
// lists them for the migration run, which creates them in every tenant and
// grants them to the admin role.
//
// The routes check them with middleware.RequirePermissionOrSystemAdmin: a
// system administrator (the is_admin flag or the admin role) passes without
// the code, anyone else needs it granted through a role.
const (
	// PermissionSystemAdmin opens the system administration: users and roles,
	// security profiles, system settings, integrations, automation, the
	// processing tools and tenant monitoring. Its holder can grant roles,
	// itself included, so it is equivalent to the admin role.
	PermissionSystemAdmin = "system:admin"
	// PermissionEventLogRead opens the event log.
	PermissionEventLogRead = "event_log:read"
	// PermissionWorkerJobRead opens the worker job monitor.
	PermissionWorkerJobRead = "worker_job:read"
)

// SystemPermissions lists the system administration permissions.
func SystemPermissions() []Permission {
	return []Permission{
		{Code: PermissionSystemAdmin, Name: "Администрирование системы", Resource: "system", Action: "admin"},
		{Code: PermissionEventLogRead, Name: "Журнал событий: чтение", Resource: "event_log", Action: "read"},
		{Code: PermissionWorkerJobRead, Name: "Задачи воркера: чтение", Resource: "worker_job", Action: "read"},
	}
}

// SystemPermissionCodes are the codes of SystemPermissions.
func SystemPermissionCodes() []string {
	permissions := SystemPermissions()
	codes := make([]string, len(permissions))
	for i, p := range permissions {
		codes[i] = p.Code
	}
	return codes
}
