package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

type Env string

const (
	Local Env = "local"
	Dev   Env = "dev"
	Prod  Env = "prod"
)

type Level string

const (
	undefinedLevel Level = ""
	DEBUG          Level = "debug"
	INFO           Level = "info"
	WARN           Level = "warn"
	ERROR          Level = "error"
)

func New(env Env, levelStr Level) (*slog.Logger, error) {
	return NewWithWriter(env, levelStr, os.Stderr)
}

func NewWithWriter(env Env, levelStr Level, w io.Writer) (*slog.Logger, error) {
	level, err := parseLevel(levelStr)
	if err != nil {
		return nil, fmt.Errorf("parse level: %w", err)
	}

	opts := &slog.HandlerOptions{Level: level}

	switch env {
	case Local, Dev:
		return slog.New(slog.NewTextHandler(w, opts)), nil
	default:
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	}
}

func NewTextLogger(level slog.Leveler, w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

func NewJSONLogger(level slog.Leveler, w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

func parseLevel(levelStr Level) (slog.Level, error) {
	switch levelStr {
	case DEBUG:
		return slog.LevelDebug, nil
	case INFO, undefinedLevel:
		return slog.LevelInfo, nil
	case WARN:
		return slog.LevelWarn, nil
	case ERROR:
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid level %q", levelStr)
	}
}

func EnvFromCfg(cfgEnv string) Env {
	switch cfgEnv {
	case "local":
		return Local
	case "dev":
		return Dev
	case "prod":
		return Prod
	default:
		return Prod
	}
}

func LogLevelFromCfg(cfgLevel string) Level {
	switch cfgLevel {
	case "info":
		return INFO
	case "debug":
		return DEBUG
	case "warn":
		return WARN
	case "error":
		return ERROR
	default:
		return INFO
	}
}
