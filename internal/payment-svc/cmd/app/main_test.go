package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commoncfg "github.com/shrtyk/e-commerce-platform/internal/common/config"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/config"
)

func TestSplitAndTrimKafkaBrokers(t *testing.T) {
	got := splitAndTrimKafkaBrokers(" broker-1:9092,broker-2:9092 , ,  broker-3:9092 ")

	require.Equal(t, []string{"broker-1:9092", "broker-2:9092", "broker-3:9092"}, got)
}

func TestCreateRelayWorkerDisabledSkipsBootstrap(t *testing.T) {
	cfg := newRelayTestConfig(false, "", "")

	worker, kafkaClient, err := createRelayWorker(cfg, nil)

	require.NoError(t, err)
	require.Nil(t, worker)
	require.Nil(t, kafkaClient)
}

func TestCreateRelayWorkerEnabledInvalidKafkaBrokers(t *testing.T) {
	cfg := newRelayTestConfig(true, "   ,  ", "http://schema-registry:8081")

	worker, kafkaClient, err := createRelayWorker(cfg, nil)

	require.Error(t, err)
	require.ErrorContains(t, err, "create relay kafka client")
	require.Nil(t, worker)
	require.Nil(t, kafkaClient)
}

func newRelayTestConfig(enabled bool, brokers, schemaRegistryURL string) config.Config {
	return config.Config{
		Config: commoncfg.Config{
			Kafka: commoncfg.Kafka{Brokers: brokers},
			SchemaRegistry: commoncfg.SchemaRegistry{
				URL: schemaRegistryURL,
			},
		},
		Relay: config.Relay{
			Enabled:          enabled,
			BatchSize:        100,
			Interval:         500 * time.Millisecond,
			RetryBaseBackoff: time.Second,
			RetryMaxBackoff:  30 * time.Second,
			WorkerID:         "payment-svc-relay-1",
			StaleLockTTL:     30 * time.Second,
		},
	}
}
