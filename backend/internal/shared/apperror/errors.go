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
