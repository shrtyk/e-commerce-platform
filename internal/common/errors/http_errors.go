package errors

import (
	"encoding/json"
	"errors"
	"net/http"
)

type HTTPError struct {
	Code       string
	Message    string
	HTTPStatus int
	err        error
}

func NewHTTPError(code, message string, httpStatus int) HTTPError {
	return HTTPError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		err:        errors.New(message),
	}
}

func (e HTTPError) Error() string {
	return e.Message
}

func BadRequest(code, message string) HTTPError {
	return NewHTTPError(code, message, http.StatusBadRequest)
}

func Unauthorized(code, message string) HTTPError {
	return NewHTTPError(code, message, http.StatusUnauthorized)
}

func NotFound(code, message string) HTTPError {
	return NewHTTPError(code, message, http.StatusNotFound)
}

func Conflict(code, message string) HTTPError {
	return NewHTTPError(code, message, http.StatusConflict)
}

func InternalError(code string) HTTPError {
	return NewHTTPError(code, "Internal server error", http.StatusInternalServerError)
}

func WriteJSON(w http.ResponseWriter, err HTTPError, correlationID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)

	payload := struct {
		Code          string `json:"code"`
		Message       string `json:"message"`
		CorrelationID string `json:"correlationId"`
	}{
		Code:          err.Code,
		Message:       err.Message,
		CorrelationID: correlationID,
	}

	_ = json.NewEncoder(w).Encode(payload)
}

func FromError(err error) HTTPError {
	if err == nil {
		return InternalError("internal_error")
	}

	if httpErr, ok := errors.AsType[HTTPError](err); ok {
		return httpErr
	}

	if httpErrPtr, ok := errors.AsType[*HTTPError](err); ok && httpErrPtr != nil {
		return *httpErrPtr
	}

	return InternalError("internal_error")
}
