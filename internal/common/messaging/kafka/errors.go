package kafka

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/sr"
)

type RetriableError struct {
	err error
}

func (e RetriableError) Error() string {
	return e.err.Error()
}

func (e RetriableError) Unwrap() error {
	return e.err
}

type NonRetriableError struct {
	err error
}

func (e NonRetriableError) Error() string {
	return e.err.Error()
}

func (e NonRetriableError) Unwrap() error {
	return e.err
}

func IsRetriable(err error) bool {
	if err == nil {
		return false
	}

	_, ok := errors.AsType[RetriableError](err)
	return ok
}

func IsNonRetriable(err error) bool {
	if err == nil {
		return false
	}

	_, ok := errors.AsType[NonRetriableError](err)
	return ok
}

func ClassifyError(err error) error {
	if err == nil {
		return nil
	}

	if IsRetriable(err) || IsNonRetriable(err) {
		return err
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return NonRetriableError{err: err}
	}

	if kerr.IsRetriable(err) {
		return RetriableError{err: err}
	}

	if responseErr, ok := errors.AsType[*sr.ResponseError](err); ok {
		if sr.IsServerError(responseErr.ErrorCode) {
			return RetriableError{err: err}
		}

		return NonRetriableError{err: err}
	}

	if netErr, ok := errors.AsType[net.Error](err); ok {
		if netErr.Timeout() {
			return RetriableError{err: err}
		}
	}

	return NonRetriableError{err: err}
}

func wrapRetriable(err error, msg string) error {
	if err == nil {
		return nil
	}

	return RetriableError{err: fmt.Errorf("%s: %w", msg, err)}
}

func wrapNonRetriable(err error, msg string) error {
	if err == nil {
		return nil
	}

	return NonRetriableError{err: fmt.Errorf("%s: %w", msg, err)}
}
