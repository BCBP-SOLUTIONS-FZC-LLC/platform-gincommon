package fixtures

import (
	"net/http"
)

// ApplyHeaders sets all entries on req.Header.
func ApplyHeaders(req *http.Request, headers map[string]string) {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

// HeadersUserRole returns gateway headers with the user role only.
func HeadersUserRole() map[string]string {
	return map[string]string{
		"x-user-id":      "user-1",
		"x-tenant-id":    "tenant-1",
		"x-tenant-roles": "user",
	}
}

// HeadersAdminRole returns gateway headers with admin (and user) roles.
func HeadersAdminRole() map[string]string {
	return DefaultHeaders()
}
