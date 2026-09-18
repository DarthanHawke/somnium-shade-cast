package domain

import (
	"errors"
	"fmt"
)

// Error — доменная ошибка
type Error struct {
	Code    string
	Message string
}

// Error - возвращает Error в виде строки
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ValidationError — ошибка валидации с указанием поля.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation: %s: %s", e.Field, e.Msg)
}

func IsInvalid(target error) bool {
	return target == ErrInvalidInput
}

func Invalid(field, msg string) error {
	return &ValidationError{
		Field: field,
		Msg:   msg,
	}
}

// WrappedError — ошибка с контекстом операции.
type WrappedError struct {
	Op  string
	Err error
}

// Error - возвращает WrappedError в виде строки
func (e *WrappedError) Error() string {
	if e.Err == nil {
		return e.Op
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

// Unwrap - возвращает ошибку типа Error содержащуюся в WrappedError
func (e *WrappedError) Unwrap() error {
	return e.Err
}

// TerminalError — ошибка, требующая ручного вмешательства.
type TerminalError struct {
	Op      string
	Context string
	Err     error
}

// Error - возвращает TerminalError в виде строки
func (e *TerminalError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("TERMINAL %s: %s", e.Op, e.Context)
	}
	return fmt.Sprintf("TERMINAL %s: %s: %v", e.Op, e.Context, e.Err)
}

// Unwrap - возвращает ошибку типа Error содержащуюся в TerminalError
func (e *TerminalError) Unwrap() error {
	return e.Err
}

// Сентинельные ошибки
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrForbidden     = errors.New("forbidden")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrInvalidInput  = errors.New("invalid input")
)

// SuccessResponse - общий успешный ответ
type SuccessResponse struct {
	Message string `json:"message" example:"operation completed successfully"`
}

// ErrorResponse - ошибка
type ErrorResponse struct {
	Error   string `json:"error" example:"invalid request"`
	Code    string `json:"code,omitempty" example:"VALIDATION_ERROR"`
	Details string `json:"details,omitempty"`
}
