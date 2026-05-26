package port

// Authorizer performs route-level authorization using user id, roles, action, and resource.
type Authorizer interface {
	Authorize(userID string, roles []string, action string, resource string) error
}
