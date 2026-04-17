package checkout

import (
	"errors"
	"fmt"
)

type CheckoutErrorCode string

const (
	CheckoutErrorCodeInvalidArgument            CheckoutErrorCode = "INVALID_ARGUMENT"
	CheckoutErrorCodeCartNotFound               CheckoutErrorCode = "CART_NOT_FOUND"
	CheckoutErrorCodeCartEmpty                  CheckoutErrorCode = "CART_EMPTY"
	CheckoutErrorCodeSKUNotFound                CheckoutErrorCode = "SKU_NOT_FOUND"
	CheckoutErrorCodeStockUnavailable           CheckoutErrorCode = "STOCK_UNAVAILABLE"
	CheckoutErrorCodePaymentDeclined            CheckoutErrorCode = "PAYMENT_DECLINED"
	CheckoutErrorCodeWrongIdempotencyKeyPayload CheckoutErrorCode = "IDEMPOTENCY_KEY_REUSED_WITH_DIFFERENT_PAYLOAD"
	CheckoutErrorCodeConflict                   CheckoutErrorCode = "CONFLICT"
	CheckoutErrorCodeInternal                   CheckoutErrorCode = "INTERNAL"
)

type CheckoutError struct {
	Code CheckoutErrorCode
	Err  error
}

func (e *CheckoutError) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Err == nil {
		return string(e.Code)
	}

	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *CheckoutError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

func CodeOf(err error) CheckoutErrorCode {
	if e, ok := errors.AsType[*CheckoutError](err); ok {
		return e.Code
	}

	return ""
}

func newCodeError(code CheckoutErrorCode, message string) error {
	return &CheckoutError{
		Code: code,
		Err:  errors.New(message),
	}
}
