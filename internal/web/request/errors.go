package request

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorResponse is the standard error format. Code is a stable, short
// machine-readable identifier that clients can branch on (e.g., display a
// specific retry UI for "spending_frozen" vs a generic toast for others).
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
}

// Error code constants. Keep this list short and stable -- adding a code is
// cheap; renaming one breaks every frontend that depends on it.
const (
	ErrCodeInvalidInput   = "invalid_input"
	ErrCodeNotFound       = "not_found"
	ErrCodeUnauthorized   = "unauthorized"
	ErrCodeForbidden      = "forbidden"
	ErrCodeConflict       = "conflict"
	ErrCodeTooLarge       = "too_large"
	ErrCodeTooManyReq     = "too_many_requests"
	ErrCodeSpendingFrozen = "spending_frozen"
	ErrCodeInternal       = "internal"
)

// WriteError writes a JSON error response without an explicit code. Prefer
// WriteCodedError for new code; this helper stays for backwards compat
// with the bulk of existing handlers.
func WriteError(w http.ResponseWriter, statusCode int, message string) {
	WriteCodedError(w, statusCode, codeForStatus(statusCode), message)
}

// WriteCodedError writes a JSON error response with an explicit machine
// code so the frontend can branch on stable identifiers rather than
// substring-matching error messages.
func WriteCodedError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(ErrorResponse{Success: false, Error: message, Code: code}); err != nil {
		slog.Error("Failed to write error response", "error", err)
	}
}

// codeForStatus maps a raw HTTP status to a sensible default error code so
// callers that use plain WriteError still populate a code. New handlers that
// need specific codes (e.g., spending_frozen) should use WriteCodedError.
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return ErrCodeInvalidInput
	case http.StatusNotFound:
		return ErrCodeNotFound
	case http.StatusUnauthorized:
		return ErrCodeUnauthorized
	case http.StatusForbidden:
		return ErrCodeForbidden
	case http.StatusConflict:
		return ErrCodeConflict
	case http.StatusRequestEntityTooLarge:
		return ErrCodeTooLarge
	case http.StatusTooManyRequests:
		return ErrCodeTooManyReq
	case http.StatusInternalServerError:
		return ErrCodeInternal
	}
	return ""
}

// WriteJSON writes a JSON success response.
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("Failed to write JSON response", "error", err)
	}
}

// SuccessResponse is a generic success wrapper.
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
}

// WriteSuccess writes a success JSON response.
func WriteSuccess(w http.ResponseWriter, data interface{}) {
	WriteJSON(w, http.StatusOK, SuccessResponse{Success: true, Data: data})
}
