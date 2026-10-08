package erp

import (
	"errors"
	"fmt"
)

// Domain errors every adapter maps its backend failures onto.
var (
	ErrNotFound        = errors.New("erp: not found")
	ErrForbidden       = errors.New("erp: forbidden")
	ErrUnauthenticated = errors.New("erp: unauthenticated")
	ErrConflict        = errors.New("erp: conflict")
	ErrUnavailable     = errors.New("erp: unavailable")
	// ErrValidation matches any *ValidationError through errors.Is.
	ErrValidation = errors.New("erp: validation failed")
)

// ValidationError reports input the ERP rejected; Fields maps a field (or
// area such as "workflow") to a human-readable reason.
type ValidationError struct {
	Message string
	Fields  map[string]string
}

func (e *ValidationError) Error() string {
	if e.Message == "" {
		return ErrValidation.Error()
	}
	return fmt.Sprintf("%s: %s", ErrValidation, e.Message)
}

// Is makes errors.Is(err, ErrValidation) true.
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// NewValidation builds a *ValidationError for a single field.
func NewValidation(field, message string) *ValidationError {
	return &ValidationError{Message: message, Fields: map[string]string{field: message}}
}
