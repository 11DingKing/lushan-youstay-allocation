package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalid           = errors.New("invalid input")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrForbidden         = errors.New("forbidden")
	ErrExpired           = errors.New("expired")
	ErrUnavailable       = errors.New("unavailable")
	ErrVersionConflict   = errors.New("version conflict")
	ErrInvalidTransition = errors.New("invalid transition")
)

type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }
func (e *FieldError) Unwrap() error { return ErrInvalid }

type ConflictError struct {
	Resource string
	Reason   string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s conflict: %s", e.Resource, e.Reason)
}
func (e *ConflictError) Unwrap() error { return ErrConflict }

type StateError struct {
	Entity string
	From   string
	To     string
}

func (e *StateError) Error() string {
	return fmt.Sprintf("%s cannot transition from %s to %s", e.Entity, e.From, e.To)
}
func (e *StateError) Unwrap() error { return ErrInvalidTransition }

func Require(condition bool, field, message string) error {
	if condition {
		return nil
	}
	return &FieldError{Field: field, Message: message}
}
