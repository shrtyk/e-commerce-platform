package http

import (
	commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
)

func errInvalidRequestParameters() error {
	return commonerrors.BadRequest(string(checkout.CheckoutErrorCodeInvalidArgument), "invalid request parameters")
}

func errInvalidRequestBody() error {
	return commonerrors.BadRequest(string(checkout.CheckoutErrorCodeInvalidArgument), "invalid request body")
}
