package domain

type Availability string

const (
	Available          Availability = "available"
	Partial            Availability = "partial"
	Unknown            Availability = "unknown"
	Unsupported        Availability = "unsupported"
	PermissionRequired Availability = "permission_required"
	DependencyMissing  Availability = "dependency_missing"
	Error              Availability = "error"
	Stale              Availability = "stale"
)
