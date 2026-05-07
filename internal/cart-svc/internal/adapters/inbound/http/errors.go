package http

import commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"

const (
	invalidRequestCode = "invalid_request"
)

func errInvalidRequestParameters() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid request parameters")
}

func errInvalidRequestBody() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid request body")
}

func errInvalidCartInput() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid cart input")
}
