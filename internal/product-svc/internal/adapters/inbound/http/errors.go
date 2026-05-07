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

func errInvalidProductID() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid product id")
}

func errInvalidCategoryID() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid category id")
}

func errInvalidCurrency() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid currency")
}

func errInvalidRequest() error {
	return commonerrors.BadRequest(invalidRequestCode, "invalid request")
}
