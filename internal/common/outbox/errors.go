package outbox

import "errors"

var (
	ErrInvalidRecord           = errors.New("outbox invalid record")
	ErrRecordNotFound          = errors.New("outbox record not found")
	ErrAlreadyClaimed          = errors.New("outbox record already claimed")
	ErrInvalidStatusTransition = errors.New("outbox invalid status transition")
	ErrPublishConflict         = errors.New("outbox publish conflict")

	ErrInvalidClaimParams                = errors.New("outbox invalid claim params")
	ErrInvalidMarkPublishedParams        = errors.New("outbox invalid mark published params")
	ErrInvalidMarkRetryableFailureParams = errors.New("outbox invalid mark retryable failure params")
	ErrInvalidMarkDeadParams             = errors.New("outbox invalid mark dead params")
	ErrInvalidRelayConfig                = errors.New("outbox invalid relay config")

	ErrInvalidIdempotencyKey = errors.New("outbox invalid idempotency key")
	ErrIdempotencyConflict   = errors.New("outbox idempotency conflict")
)
