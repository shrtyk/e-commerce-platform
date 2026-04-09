package errors

import (
	"encoding/json"
	stdErrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewHTTPError(t *testing.T) {
	type wantResult struct {
		code       string
		message    string
		httpStatus int
	}
	tests := []struct {
		name string
		got  HTTPError
		want wantResult
	}{
		{
			name: "new",
			got:  NewHTTPError("bad_request", "invalid input", http.StatusBadRequest),
			want: wantResult{
				code:       "bad_request",
				message:    "invalid input",
				httpStatus: http.StatusBadRequest,
			},
		},
		{
			name: "bad request",
			got:  BadRequest("invalid_input", "invalid input"),
			want: wantResult{
				code:       "invalid_input",
				message:    "invalid input",
				httpStatus: http.StatusBadRequest,
			},
		},
		{
			name: "unauthorized",
			got:  Unauthorized("invalid_credentials", "invalid credentials"),
			want: wantResult{
				code:       "invalid_credentials",
				message:    "invalid credentials",
				httpStatus: http.StatusUnauthorized,
			},
		},
		{
			name: "not found",
			got:  NotFound("not_found", "resource not found"),
			want: wantResult{
				code:       "not_found",
				message:    "resource not found",
				httpStatus: http.StatusNotFound,
			},
		},
		{
			name: "conflict",
			got:  Conflict("email_already_registered", "email already registered"),
			want: wantResult{
				code:       "email_already_registered",
				message:    "email already registered",
				httpStatus: http.StatusConflict,
			},
		},
		{
			name: "internal",
			got:  InternalError("internal_error"),
			want: wantResult{
				code:       "internal_error",
				message:    "Internal server error",
				httpStatus: http.StatusInternalServerError,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want.code, tt.got.Code)
			require.Equal(t, tt.want.message, tt.got.Message)
			require.Equal(t, tt.want.httpStatus, tt.got.HTTPStatus)
			require.Equal(t, tt.want.message, tt.got.Error())
		})
	}
}

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name          string
		err           HTTPError
		correlationID string
		wantStatus    int
		wantCode      string
		wantMessage   string
	}{
		{
			name:          "conflict",
			err:           Conflict("email_already_registered", "email already registered"),
			correlationID: "corr-123",
			wantStatus:    http.StatusConflict,
			wantCode:      "email_already_registered",
			wantMessage:   "email already registered",
		},
		{
			name:          "internal",
			err:           InternalError("internal_error"),
			correlationID: "corr-456",
			wantStatus:    http.StatusInternalServerError,
			wantCode:      "internal_error",
			wantMessage:   "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			WriteJSON(rec, tt.err, tt.correlationID)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			var payload map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
			require.Equal(t, tt.wantCode, payload["code"])
			require.Equal(t, tt.wantMessage, payload["message"])
			require.Equal(t, tt.correlationID, payload["correlationId"])
		})
	}
}

func TestFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want HTTPError
	}{
		{
			name: "http error as is",
			err:  Conflict("email_already_registered", "email already registered"),
			want: Conflict("email_already_registered", "email already registered"),
		},
		{
			name: "unknown",
			err:  stdErrors.New("boom"),
			want: BadRequest("invalid_request", "boom"),
		},
		{
			name: "nil",
			err:  nil,
			want: InternalError("unknown"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromError(tt.err)

			require.Equal(t, tt.want.Code, got.Code)
			require.Equal(t, tt.want.Message, got.Message)
			require.Equal(t, tt.want.HTTPStatus, got.HTTPStatus)
		})
	}
}
