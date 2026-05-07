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

func errInvalidRegisterInput() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid register input")
}

func errInvalidProfileInput() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid profile input")
}
