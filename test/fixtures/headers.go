package fixtures

// DefaultHeaders returns standard gateway identity headers for integration tests.
func DefaultHeaders() map[string]string {
	return map[string]string{
		"x-user-id":      "user-1",
		"x-tenant-id":    "tenant-1",
		"x-tenant-roles": "admin,user",
	}
}
