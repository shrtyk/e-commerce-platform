package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewWithWriterSelectsHandler(t *testing.T) {
	tests := []struct {
		name     string
		env      Env
		wantText bool
	}{
		{
			name:     "local",
			env:      Local,
			wantText: true,
		},
		{
			name:     "dev",
			env:      Dev,
			wantText: true,
		},
		{
			name:     "prod",
			env:      Prod,
			wantText: false,
		},
		{
			name:     "unknown",
			env:      "unknown",
			wantText: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger, err := NewWithWriter(tt.env, INFO, &buf)
			require.NoError(t, err)
			logger.Info("test message", "key", "value")

			if tt.wantText {
				output := buf.String()
				require.Contains(t, output, "level=INFO")
				require.Contains(t, output, `msg="test message"`)
				require.Contains(t, output, "key=value")
			} else {
				var entry map[string]interface{}
				err = json.Unmarshal(buf.Bytes(), &entry)
				require.NoError(t, err)
				require.Equal(t, "test message", entry["msg"])
				require.Equal(t, "INFO", entry["level"])
				require.Equal(t, "value", entry["key"])
			}
		})
	}
}

func TestNewWithWriterParsesLevel(t *testing.T) {
	tests := []struct {
		name           string
		level          Level
		belowThreshold string
		atThreshold    string
		wantMsg        string
	}{
		{
			name:           "debug",
			level:          DEBUG,
			belowThreshold: "",
			atThreshold:    "debug msg",
		},
		{
			name:           "info",
			level:          INFO,
			belowThreshold: "debug msg",
			atThreshold:    "info msg",
		},
		{
			name:           "warn",
			level:          WARN,
			belowThreshold: "info msg",
			atThreshold:    "warn msg",
		},
		{
			name:           "error",
			level:          ERROR,
			belowThreshold: "warn msg",
			atThreshold:    "error msg",
		},
		{
			name:           "empty defaults to info",
			level:          undefinedLevel,
			belowThreshold: "debug msg",
			atThreshold:    "info msg",
		},
		{
			name:    "invalid",
			level:   "invalid",
			wantMsg: "invalid level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger, err := NewWithWriter(Prod, tt.level, &buf)

			if tt.wantMsg != "" {
				require.Error(t, err)
				require.True(t, strings.Contains(err.Error(), tt.wantMsg))
				return
			}

			require.NoError(t, err)

			if tt.belowThreshold != "" {
				switch tt.level {
				case INFO, undefinedLevel:
					logger.Debug(tt.belowThreshold)
				case WARN:
					logger.Info(tt.belowThreshold)
				case ERROR:
					logger.Warn(tt.belowThreshold)
				}
			}

			switch tt.level {
			case DEBUG:
				logger.Debug(tt.atThreshold)
			case INFO, undefinedLevel:
				logger.Info(tt.atThreshold)
			case WARN:
				logger.Warn(tt.atThreshold)
			case ERROR:
				logger.Error(tt.atThreshold)
			}

			var entry map[string]interface{}
			err = json.Unmarshal(buf.Bytes(), &entry)
			require.NoError(t, err)
			require.Equal(t, tt.atThreshold, entry["msg"])
		})
	}
}

func TestNewWithWriterFiltersLevel(t *testing.T) {
	var buf bytes.Buffer
	logger, err := NewWithWriter(Prod, INFO, &buf)
	require.NoError(t, err)

	logger.Debug("should not appear")
	logger.Info("should appear")

	var entries []map[string]interface{}
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	require.Len(t, entries, 1)
	require.Equal(t, "should appear", entries[0]["msg"])
}

func TestEnvFromCfg(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Env
	}{
		{
			name: "local",
			in:   "local",
			want: Local,
		},
		{
			name: "dev",
			in:   "dev",
			want: Dev,
		},
		{
			name: "prod",
			in:   "prod",
			want: Prod,
		},
		{
			name: "empty defaults to prod",
			in:   "",
			want: Prod,
		},
		{
			name: "unknown defaults to prod",
			in:   "staging",
			want: Prod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnvFromCfg(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLogLevelFromCfg(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Level
	}{
		{
			name: "info",
			in:   "info",
			want: INFO,
		},
		{
			name: "debug",
			in:   "debug",
			want: DEBUG,
		},
		{
			name: "warn",
			in:   "warn",
			want: WARN,
		},
		{
			name: "error",
			in:   "error",
			want: ERROR,
		},
		{
			name: "empty defaults to info",
			in:   "",
			want: INFO,
		},
		{
			name: "unknown defaults to info",
			in:   "trace",
			want: INFO,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LogLevelFromCfg(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}
