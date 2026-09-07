package errors

import (
	"fmt"
	"net/http"
)

// AppError represents a custom application error
type AppError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
	HTTPStatus int                    `json:"-"`
	Err        error                  `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap implements the errors.Unwrap interface
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithDetails adds details to the error
func (e *AppError) WithDetails(key string, value interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// WithError wraps an underlying error
func (e *AppError) WithError(err error) *AppError {
	e.Err = err
	return e
}

// Error codes
const (
	// Authentication & Authorization
	ErrCodeUnauthorized       = "UNAUTHORIZED"
	ErrCodeInvalidCredentials = "INVALID_CREDENTIALS" // #nosec G101 -- error code string, not a credential
	ErrCodeTokenExpired       = "TOKEN_EXPIRED"
	ErrCodeTokenInvalid       = "TOKEN_INVALID"
	ErrCodeForbidden          = "FORBIDDEN"
	// ErrCodeClientNotSupported signals that a native client's own version is
	// below the server's configured MIN_CLIENT_VERSION floor, so the server
	// refuses to authenticate it (issue #692, docs/client-compatibility-policy.md
	// "Moving the floor"). The client-side force-update screen is the UX layer
	// that pre-empts it; this is the authoritative backstop a modified client
	// that skips its own check cannot. 403 — the request is understood but the
	// policy refuses this client version.
	ErrCodeClientNotSupported = "CLIENT_NOT_SUPPORTED"

	// Resource errors
	ErrCodeNotFound      = "NOT_FOUND"
	ErrCodeAlreadyExists = "ALREADY_EXISTS"
	ErrCodeConflict      = "CONFLICT"
	// ErrCodePreconditionFailed signals that a conditional write's If-Match
	// precondition did not hold — the caller's revision token is stale
	// because the row was modified since they read it (CON-01, issue #456,
	// ADR 0008). RFC 9110's 412 Precondition Failed.
	ErrCodePreconditionFailed = "PRECONDITION_FAILED"
	// ErrCodeIdempotencyKeyReused signals that an Idempotency-Key was
	// replayed with a different request (method/path/body) than the one it
	// first identified (CON-04, issue #459, ADR 0010) — a client bug, since
	// the key no longer names a single operation. 422.
	ErrCodeIdempotencyKeyReused = "IDEMPOTENCY_KEY_REUSED"
	// ErrCodeIdempotencyInProgress signals that a request with this
	// Idempotency-Key is still being processed by an earlier call, so this
	// retry cannot be served yet (CON-04, issue #459). 409 — the client
	// retries after a short delay.
	ErrCodeIdempotencyInProgress = "IDEMPOTENCY_IN_PROGRESS"
	// ErrCodeGone signals that a requested resource (e.g. a change-feed cursor
	// older than the purge retention window) no longer exists and the client
	// must recover differently (full resync) — RFC 9110's 410 Gone.
	ErrCodeGone = "GONE"

	// Validation errors
	ErrCodeValidation   = "VALIDATION_ERROR"
	ErrCodeInvalidInput = "INVALID_INPUT"
	ErrCodeMissingField = "MISSING_FIELD"

	// Rate limiting
	ErrCodeRateLimitExceeded = "RATE_LIMIT_EXCEEDED"

	// ErrCodeInsufficientStorage signals that an operation was refused by a
	// preflight capacity check — the filesystem holding the database does not
	// have room to run it (a bulk import's transaction, a backup's second
	// copy), or the request would materialize an unbounded result in memory (a
	// full-database export beyond the safety cap). Issue #498: degrade with a
	// clear, actionable error before an ENOSPC mid-write or an OOM kill.
	// RFC 4918's 507 Insufficient Storage.
	ErrCodeInsufficientStorage = "INSUFFICIENT_STORAGE"

	// Internal errors
	ErrCodeInternal = "INTERNAL_ERROR"
	ErrCodeDatabase = "DATABASE_ERROR"
	ErrCodeExternal = "EXTERNAL_SERVICE_ERROR"

	// Business logic errors
	ErrCodeBusinessLogic   = "BUSINESS_LOGIC_ERROR"
	ErrCodeOperationFailed = "OPERATION_FAILED"
)

// NewError creates a new AppError
func NewError(code, message string, httpStatus int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Details:    make(map[string]interface{}),
	}
}

// --- Authentication & Authorization Errors ---

// ErrUnauthorized returns an unauthorized error
func ErrUnauthorized(message string) *AppError {
	if message == "" {
		message = "Authentication required"
	}
	return NewError(ErrCodeUnauthorized, message, http.StatusUnauthorized)
}

// ErrInvalidCredentials returns an invalid credentials error
func ErrInvalidCredentials() *AppError {
	return NewError(ErrCodeInvalidCredentials, "Invalid email or password", http.StatusUnauthorized)
}

// ErrTokenExpired returns a token expired error
func ErrTokenExpired() *AppError {
	return NewError(ErrCodeTokenExpired, "Authentication token has expired", http.StatusUnauthorized)
}

// ErrTokenInvalid returns a token invalid error
func ErrTokenInvalid() *AppError {
	return NewError(ErrCodeTokenInvalid, "Authentication token is invalid", http.StatusUnauthorized)
}

// ErrForbidden returns a forbidden error
func ErrForbidden(message string) *AppError {
	if message == "" {
		message = "You don't have permission to access this resource"
	}
	return NewError(ErrCodeForbidden, message, http.StatusForbidden)
}

// ErrClientNotSupported returns a 403 for a native client whose own version is
// below the server's configured MIN_CLIENT_VERSION floor (issue #692). The
// message names the required version so the operator-facing log line and any
// future client that reads the body can act on it.
func ErrClientNotSupported(requiredVersion string) *AppError {
	message := "This version of the app is no longer supported"
	if requiredVersion != "" {
		message = fmt.Sprintf("This version of the app is no longer supported; update to version %s or newer", requiredVersion)
	}
	return NewError(ErrCodeClientNotSupported, message, http.StatusForbidden)
}

// --- Resource Errors ---

// ErrNotFound returns a not found error
func ErrNotFound(resource string) *AppError {
	message := "Resource not found"
	if resource != "" {
		message = fmt.Sprintf("%s not found", resource)
	}
	return NewError(ErrCodeNotFound, message, http.StatusNotFound)
}

// ErrAlreadyExists returns an already exists error
func ErrAlreadyExists(resource string) *AppError {
	message := "Resource already exists"
	if resource != "" {
		message = fmt.Sprintf("%s already exists", resource)
	}
	return NewError(ErrCodeAlreadyExists, message, http.StatusConflict)
}

// ErrConflict returns a conflict error
func ErrConflict(message string) *AppError {
	if message == "" {
		message = "Resource conflict occurred"
	}
	return NewError(ErrCodeConflict, message, http.StatusConflict)
}

// ErrGone returns a 410 Gone error. Used by the T17 change feed: a ?since=
// cursor older than the purge retention window (T26) means tombstones have
// already been hard-deleted, so incremental sync can no longer converge —
// the client must full-resync every collection. The message must say so.
func ErrGone(message string) *AppError {
	if message == "" {
		message = "Resource no longer available — full resync required"
	}
	return NewError(ErrCodeGone, message, http.StatusGone)
}

// ErrPreconditionFailed returns a 412 Precondition Failed error for a stale
// conditional write (CON-01, issue #456, ADR 0008): the client sent an
// If-Match revision token that no longer matches the row's current revision,
// so the row has been modified since they read it and the write is rejected
// without touching the row. The message should tell the client to re-fetch.
func ErrPreconditionFailed(message string) *AppError {
	if message == "" {
		message = "Precondition failed — the resource was modified since you last read it; re-fetch and retry"
	}
	return NewError(ErrCodePreconditionFailed, message, http.StatusPreconditionFailed)
}

// ErrIdempotencyKeyReused returns a 422 for an Idempotency-Key replayed with a
// different request than the one it first identified (CON-04, issue #459,
// ADR 0010). The stored response is deliberately NOT replayed — it would be
// the wrong answer for this request.
func ErrIdempotencyKeyReused() *AppError {
	return NewError(ErrCodeIdempotencyKeyReused,
		"Idempotency-Key was already used for a different request; use a fresh key",
		http.StatusUnprocessableEntity)
}

// ErrIdempotencyInProgress returns a 409 when a request with the same
// Idempotency-Key is still being processed (CON-04, issue #459). The client
// should retry after a short delay, at which point the first request's stored
// response will be replayed.
func ErrIdempotencyInProgress() *AppError {
	return NewError(ErrCodeIdempotencyInProgress,
		"a request with this Idempotency-Key is still being processed; retry shortly",
		http.StatusConflict)
}

// --- Validation Errors ---

// ErrValidation returns a validation error
func ErrValidation(message string) *AppError {
	if message == "" {
		message = "Validation failed"
	}
	return NewError(ErrCodeValidation, message, http.StatusBadRequest)
}

// ErrInvalidInput returns an invalid input error
func ErrInvalidInput(field, reason string) *AppError {
	message := "Invalid input"
	if field != "" {
		message = fmt.Sprintf("Invalid value for field '%s'", field)
	}
	err := NewError(ErrCodeInvalidInput, message, http.StatusBadRequest)
	if reason != "" {
		err.WithDetails("reason", reason)
	}
	if field != "" {
		err.WithDetails("field", field)
	}
	return err
}

// ErrMissingField returns a missing field error
func ErrMissingField(field string) *AppError {
	message := "Required field is missing"
	if field != "" {
		message = fmt.Sprintf("Required field '%s' is missing", field)
	}
	err := NewError(ErrCodeMissingField, message, http.StatusBadRequest)
	if field != "" {
		err.WithDetails("field", field)
	}
	return err
}

// --- Rate Limiting Errors ---

// ErrRateLimitExceeded returns a rate limit exceeded error
func ErrRateLimitExceeded() *AppError {
	return NewError(ErrCodeRateLimitExceeded, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
}

// --- Capacity Errors ---

// ErrInsufficientStorage returns a 507 Insufficient Storage error — an
// operation refused by a preflight capacity check (issue #498). message should
// be operator-actionable ("not enough disk space to import 4000 contacts",
// "export of 1000000 contacts exceeds the 250000-row limit").
func ErrInsufficientStorage(message string) *AppError {
	if message == "" {
		message = "The server does not have enough storage to complete this operation"
	}
	return NewError(ErrCodeInsufficientStorage, message, http.StatusInsufficientStorage)
}

// --- Internal Errors ---

// ErrInternal returns an internal server error
func ErrInternal(message string) *AppError {
	if message == "" {
		message = "An internal error occurred"
	}
	return NewError(ErrCodeInternal, message, http.StatusInternalServerError)
}

// ErrDatabase returns a database error
func ErrDatabase(operation string) *AppError {
	message := "Database operation failed"
	if operation != "" {
		message = fmt.Sprintf("Database %s operation failed", operation)
	}
	err := NewError(ErrCodeDatabase, message, http.StatusInternalServerError)
	if operation != "" {
		err.WithDetails("operation", operation)
	}
	return err
}

// ErrExternal returns an external service error
func ErrExternal(service, message string) *AppError {
	if message == "" {
		message = "External service error"
	}
	if service != "" {
		message = fmt.Sprintf("%s service error: %s", service, message)
	}
	err := NewError(ErrCodeExternal, message, http.StatusServiceUnavailable)
	if service != "" {
		err.WithDetails("service", service)
	}
	return err
}

// --- Business Logic Errors ---

// ErrBusinessLogic returns a business logic error
func ErrBusinessLogic(message string) *AppError {
	if message == "" {
		message = "Business logic constraint violated"
	}
	return NewError(ErrCodeBusinessLogic, message, http.StatusUnprocessableEntity)
}

// ErrOperationFailed returns an operation failed error
func ErrOperationFailed(operation, reason string) *AppError {
	message := "Operation failed"
	if operation != "" {
		message = fmt.Sprintf("%s operation failed", operation)
	}
	err := NewError(ErrCodeOperationFailed, message, http.StatusUnprocessableEntity)
	if operation != "" {
		err.WithDetails("operation", operation)
	}
	if reason != "" {
		err.WithDetails("reason", reason)
	}
	return err
}

// GetAppError extracts an AppError from an error, or creates a generic internal error
func GetAppError(err error) *AppError {
	if err == nil {
		return nil
	}

	if appErr, ok := err.(*AppError); ok {
		return appErr
	}

	return ErrInternal("An unexpected error occurred").WithError(err)
}
