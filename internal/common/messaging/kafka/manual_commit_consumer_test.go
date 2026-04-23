package kafka

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type pollStub struct {
	pollCalls int
	messages  []ConsumedMessage
	err       error
}

func (s *pollStub) Poll(context.Context) ([]ConsumedMessage, error) {
	s.pollCalls++

	return s.messages, s.err
}

type commitStub struct {
	commitCalls int
	err         error
}

func (s *commitStub) CommitUncommittedOffsets(context.Context) error {
	s.commitCalls++

	return s.err
}

func TestConsumerWithManualCommitPollDelegates(t *testing.T) {
	expectedMessages := []ConsumedMessage{{}}
	poller := &pollStub{messages: expectedMessages}
	committer := &commitStub{}

	consumer, err := NewConsumerWithManualCommit(poller, committer)
	require.NoError(t, err)

	got, err := consumer.Poll(context.Background())
	require.NoError(t, err)
	require.Equal(t, expectedMessages, got)
	require.Equal(t, 1, poller.pollCalls)
}

func TestConsumerWithManualCommitCommitDelegates(t *testing.T) {
	poller := &pollStub{}
	expectedErr := errors.New("commit failed")
	committer := &commitStub{err: expectedErr}

	consumer, err := NewConsumerWithManualCommit(poller, committer)
	require.NoError(t, err)

	err = consumer.CommitUncommittedOffsets(context.Background())
	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, 1, committer.commitCalls)
}

func TestConsumerWithManualCommitValidateDependencies(t *testing.T) {
	committer := &commitStub{}
	poller := &pollStub{}

	t.Run("nil poller", func(t *testing.T) {
		consumer, err := NewConsumerWithManualCommit(nil, committer)
		require.Nil(t, consumer)
		require.EqualError(t, err, "poll consumer is nil")
	})

	t.Run("nil committer", func(t *testing.T) {
		consumer, err := NewConsumerWithManualCommit(poller, nil)
		require.Nil(t, consumer)
		require.EqualError(t, err, "offset commit client is nil")
	})
}
