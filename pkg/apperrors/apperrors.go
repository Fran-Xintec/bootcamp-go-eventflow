package apperrors

import "fmt"

type ErrorType string

// Cada tipo corresponde a un código HTTP (ver pkg/httputil/error_mapper.go):
// BadRequest = 400 (la petición no se puede interpretar), Validation = 422
// (se entendió pero el contenido incumple una regla), Conflict = 409, etc.
const (
	ErrorTypeBadRequest   ErrorType = "BAD_REQUEST"
	ErrorTypeValidation   ErrorType = "VALIDATION_ERROR"
	ErrorTypeUnsupported  ErrorType = "UNSUPPORTED_MEDIA_TYPE"
	ErrorTypeTooLarge     ErrorType = "PAYLOAD_TOO_LARGE"
	ErrorTypeUnavailable  ErrorType = "SERVICE_UNAVAILABLE"
	ErrorTypeNotFound     ErrorType = "NOT_FOUND"
	ErrorTypeConflict     ErrorType = "CONFLICT"
	ErrorTypeUnauthorized ErrorType = "UNAUTHORIZED"
	ErrorTypeForbidden    ErrorType = "FORBIDDEN"
	ErrorTypeInternal     ErrorType = "INTERNAL_ERROR"
)

// DomainError representa errores del dominio de negocio
type DomainError struct {
	Type    ErrorType `json:"type"`
	Code    string    `json:"code"`
	Message string    `json:"message"`
	Details string    `json:"details,omitempty"`
}

func (e *DomainError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Type, e.Code, e.Message)
}

// WithDetails agrega detalles adicionales al error
func (e *DomainError) WithDetails(details string) *DomainError {
	e.Details = details
	return e
}

// Constructores de errores por tipo
func NewBadRequestError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeBadRequest,
		Code:    code,
		Message: message,
	}
}

func NewUnsupportedMediaTypeError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeUnsupported,
		Code:    code,
		Message: message,
	}
}

func NewPayloadTooLargeError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeTooLarge,
		Code:    code,
		Message: message,
	}
}

func NewUnavailableError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeUnavailable,
		Code:    code,
		Message: message,
	}
}

func NewValidationError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeValidation,
		Code:    code,
		Message: message,
	}
}

func NewNotFoundError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeNotFound,
		Code:    code,
		Message: message,
	}
}

func NewConflictError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeConflict,
		Code:    code,
		Message: message,
	}
}

func NewUnauthorizedError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeUnauthorized,
		Code:    code,
		Message: message,
	}
}

func NewForbiddenError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeForbidden,
		Code:    code,
		Message: message,
	}
}

func NewInternalError(code, message string) *DomainError {
	return &DomainError{
		Type:    ErrorTypeInternal,
		Code:    code,
		Message: message,
	}
}
