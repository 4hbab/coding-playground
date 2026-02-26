package apperror

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound = errors.New("not found")
	ErrValidation = errors.New("Validation Error")
	ErrConflict = errors.New("conflict")
)

type AppError struct {
	Err		error  // actual error
	Message	string // Human-readable error message
	Field	string // Optional: field causing the error
}

func (e *AppError) Error() string {
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NotFound(resource, id string) *AppError {
	return &AppError{
		Err: ErrNotFound,
		Message: fmt.Sprintf("%s not found with id %s", resource, id),
	}
}

func ValidationFailed(field, message string) *AppError {
	return &AppError{
		Err: ErrValidation,
		Message: message,
		Field: field,
	}
}

func Conflict(resource, id string) *AppError {
	return &AppError{
		Err: ErrConflict,
		Message: fmt.Sprintf("%s conflict with id %s", resource, id),
	}
}