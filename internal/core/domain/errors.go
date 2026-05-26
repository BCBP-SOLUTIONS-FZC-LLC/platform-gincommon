package domain

import "errors"

// Domain errors returned by context and authorization services.
var (
	// ErrMissingTenant indicates x-tenant-id was empty after validation.
	ErrMissingTenant = errors.New("missing tenant id")
	// ErrMissingUser indicates x-user-id was empty after validation.
	ErrMissingUser = errors.New("missing user id")
	// ErrForbidden indicates the authorizer denied the action.
	ErrForbidden = errors.New("forbidden")
)
