package kafka

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/sr"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		retriable    bool
		nonRetriable bool
	}{
		{
			name:         "kafka retriable",
			err:          kerr.LeaderNotAvailable,
			retriable:    true,
			nonRetriable: false,
		},
		{
			name:         "kafka non-retriable",
			err:          kerr.InvalidTopicException,
			retriable:    false,
			nonRetriable: true,
		},
		{
			name: "schema registry server error",
			err: &sr.ResponseError{
				StatusCode: 500,
				ErrorCode:  50001,
				Message:    "internal",
			},
			retriable:    true,
			nonRetriable: false,
		},
		{
			name: "schema registry client error",
			err: &sr.ResponseError{
				StatusCode: 422,
				ErrorCode:  42201,
				Message:    "invalid schema",
			},
			retriable:    false,
			nonRetriable: true,
		},
		{
			name:         "context canceled",
			err:          context.Canceled,
			retriable:    false,
			nonRetriable: true,
		},
		{
			name:         "wrapped context canceled",
			err:          fmt.Errorf("poll: %w", context.Canceled),
			retriable:    false,
			nonRetriable: true,
		},
		{
			name:         "net timeout",
			err:          testNetError{msg: "timeout", timeout: true},
			retriable:    true,
			nonRetriable: false,
		},
		{
			name:         "net temporary",
			err:          testNetError{msg: "temporary", temporary: true},
			retriable:    true,
			nonRetriable: false,
		},
		{
			name:         "generic error",
			err:          errors.New("boom"),
			retriable:    false,
			nonRetriable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classified := ClassifyError(tt.err)
			require.Equal(t, tt.retriable, IsRetriable(classified))
			require.Equal(t, tt.nonRetriable, IsNonRetriable(classified))
		})
	}
}

type testNetError struct {
	msg       string
	timeout   bool
	temporary bool
}

func (e testNetError) Error() string {
	return e.msg
}

func (e testNetError) Timeout() bool {
	return e.timeout
}

func (e testNetError) Temporary() bool {
	return e.temporary
}
