package domain

import (
	"errors"
	"fmt"
)

// ValidationError — ошибка валидации с указанием поля
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation: %s: %s", e.Field, e.Message)
}

func Invalid(field, message string) error {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

// Предопределенные ошибки
// Ошибки общие:
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
)

// Ошибки Blobs:
var (
	// ErrBlobAlreadyExist - уже зарегистрирован блоб с таким SHA-256
	ErrBlobAlreadyExist = fmt.Errorf("blob: %w", ErrConflict)
	// ErrAlreadyOrphan - на блоб уже ничего не ссылается
	ErrAlreadyOrphan = errors.New("blob already orphan")
	// ErrBlobInUse - блоб уже используется
	ErrBlobInUse = errors.New("blob in use")
	// ErrNoBlobAvailable - нет доступных блобов
	ErrNoBlobAvailable = errors.New("no blob available")
)

// Ошибки Storage
var (
	// ErrBlobFileMissing - запись в БД есть, а файла на диске нет
	ErrBlobFileMissing = errors.New("blob file missing")
	// ErrTampered - зашифрованный блоб повреждён или подменён
	ErrTampered = errors.New("blob tampered")
	// ErrDecryptionFailed - AEAD не смог расшифровать (тег не сошёлся)
	ErrDecryptionFailed = errors.New("decryption failed")
	// ErrInvalidWrappedKey - обёрнутый ключ структурно некорректен
	ErrInvalidWrappedKey = errors.New("invalid wrapped key")
)

// Валидация:
var (
	// ErrRangeNotSatisfiable  - Range выходит за пределы blob
	ErrRangeNotSatisfiable = errors.New("range not satisfiable")
	// ErrTooLarge - превышен допустимый размер загрузки
	ErrTooLarge = errors.New("too large")
	// ErrIncomplete - данные пришли не полностью
	ErrIncomplete = errors.New("incomplete upload")
	// ErrInvalidMasterKey - мастер-ключ не соответствует требованиям
	ErrInvalidMasterKey = errors.New("invalid master key")
)
