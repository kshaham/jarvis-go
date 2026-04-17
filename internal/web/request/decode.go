package request

import (
	"encoding/json"
	"errors"
	"net/http"
)

// defaultMaxBodyBytes bounds how much JSON we'll accept on any handler that
// uses DecodeAndValidate. 1MB matches the per-handler cap already applied
// inline across the codebase.
const defaultMaxBodyBytes = 1 << 20

// Validator is implemented by request structs that want field-level
// validation on decode. Return nil when the payload is well-formed; any
// non-nil error becomes a 400 with the error's message.
//
// Example:
//
//	type Req struct{ Name string `json:"name"` }
//	func (r Req) Validate() error {
//	    if r.Name == "" { return errors.New("name is required") }
//	    return nil
//	}
type Validator interface {
	Validate() error
}

// DecodeAndValidate reads + decodes the request body into dst and, if dst
// implements Validator, runs Validate(). On any failure it writes the
// appropriate 4xx response and returns false so the caller can early-return:
//
//	var req Req
//	if !request.DecodeAndValidate(w, r, &req) { return }
//
// Applies a 1MB body cap unless the caller has already wrapped r.Body.
func DecodeAndValidate(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	return DecodeAndValidateMax(w, r, dst, defaultMaxBodyBytes)
}

// DecodeAndValidateMax is like DecodeAndValidate but accepts a custom body
// cap for endpoints that need to handle larger payloads (e.g. import /
// bulk operations).
func DecodeAndValidateMax(w http.ResponseWriter, r *http.Request, dst interface{}, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		// Distinguish oversized body (413) from malformed JSON (400) so
		// callers get a more accurate signal.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		WriteError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	if v, ok := dst.(Validator); ok {
		if err := v.Validate(); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return false
		}
	}
	return true
}
