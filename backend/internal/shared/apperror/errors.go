// Package apperror contains stable application error categories.
package apperror

import "errors"

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrBusinessRule    = errors.New("business rule violation")
	ErrUnavailable     = errors.New("service unavailable")
)

// Detail is safe, field-level validation information intended for public API
// responses. It must never contain source values, SQL, or implementation text.
type Detail struct {
	Field   string
	Code    string
	Message string
}

// Typed wraps a stable category and optional safe public details. Responder
// classification uses errors.As for this type and errors.Is for its category.
type Typed struct {
	Category error
	Details  []Detail
}

func (e *Typed) Error() string { return e.Category.Error() }
func (e *Typed) Unwrap() error { return e.Category }
