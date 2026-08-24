package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/observability"
)

type errorBody struct {
	Error apiError `json:"error"`
}
type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &domain.FieldError{Field: "body", Message: "invalid JSON"}
	}
	return nil
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "the request could not be completed"
	switch {
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrExpired):
		status, code, message = http.StatusUnauthorized, "unauthorized", "authentication is required or has expired"
	case errors.Is(err, domain.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "the current role cannot perform this action"
	case errors.Is(err, domain.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "the requested object was not found"
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrVersionConflict), errors.Is(err, domain.ErrInvalidTransition):
		status, code, message = http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, domain.ErrInvalid):
		status, code, message = http.StatusBadRequest, "invalid_request", err.Error()
	case errors.Is(err, domain.ErrUnavailable):
		status, code, message = http.StatusServiceUnavailable, "unavailable", "the requested resource is temporarily unavailable"
	}
	writeJSON(w, status, errorBody{Error: apiError{Code: code, Message: message, RequestID: observability.RequestID(r.Context())}})
}
