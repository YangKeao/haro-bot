package logging

import (
	"errors"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Config struct {
	Level       string
	Development bool
	Encoding    string
}

var (
	mu     sync.Mutex
	logger *zap.Logger
)

func Init(cfg Config) (*zap.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	var zapCfg zap.Config
	if cfg.Development {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
	}
	zapCfg.Level = zap.NewAtomicLevelAt(level)
	if cfg.Encoding != "" {
		zapCfg.Encoding = cfg.Encoding
	}
	l, err := zapCfg.Build()
	if err != nil {
		return nil, err
	}
	Set(l)
	return l, nil
}

func InitFromEnv() (*zap.Logger, error) {
	return Init(ConfigFromEnv())
}

func ConfigFromEnv() Config {
	level := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	encoding := strings.TrimSpace(os.Getenv("LOG_ENCODING"))
	dev := envBool(os.Getenv("LOG_DEV"))
	return Config{
		Level:       level,
		Development: dev,
		Encoding:    encoding,
	}
}

func Set(l *zap.Logger) {
	if l == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	logger = l
}

func L() *zap.Logger {
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		logger = zap.NewNop()
	}
	return logger
}

func S() *zap.SugaredLogger {
	return L().Sugar()
}

func parseLevel(level string) (zapcore.Level, error) {
	if strings.TrimSpace(level) == "" {
		return zapcore.InfoLevel, nil
	}
	var out zapcore.Level
	if err := out.Set(strings.ToLower(strings.TrimSpace(level))); err != nil {
		return zapcore.InfoLevel, errors.New("invalid log level")
	}
	return out, nil
}

func envBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
