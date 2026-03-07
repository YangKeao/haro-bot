package server

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/go-telegram/bot"
	"go.uber.org/zap"
)

const (
	telegramMaxRetries   = 3
	telegramRetryPadding = 200 * time.Millisecond
)

var (
	retryAfterPattern    = regexp.MustCompile(`(?i)retry after\s+(\d+)`)
	retryAfterAltPattern = regexp.MustCompile(`(?i)retry_after\s*[:=]?\s*(\d+)`)
)

func withTelegramRetry(ctx context.Context, log *zap.Logger, op string, fn func(context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt <= telegramMaxRetries; attempt++ {
		if attempt > 0 {
			wait, ok := retryAfterFromError(lastErr)
			if !ok {
				return lastErr
			}
			if wait > 0 {
				wait += telegramRetryPadding
			}
			if log != nil {
				log.Debug("telegram rate limited, retrying", zap.String("op", op), zap.Duration("retry_after", wait), zap.Int("attempt", attempt))
			}
			if !sleepWithContext(ctx, wait) {
				return lastErr
			}
		}
		if err := fn(ctx); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func retryAfterFromError(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	var tooMany *bot.TooManyRequestsError
	if errors.As(err, &tooMany) {
		if tooMany.RetryAfter > 0 {
			return time.Duration(tooMany.RetryAfter) * time.Second, true
		}
	}
	return parseRetryAfter(err.Error())
}

func parseRetryAfter(text string) (time.Duration, bool) {
	if text == "" {
		return 0, false
	}
	if m := retryAfterPattern.FindStringSubmatch(text); len(m) == 2 {
		if secs, err := strconv.Atoi(m[1]); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second, true
		}
	}
	if m := retryAfterAltPattern.FindStringSubmatch(text); len(m) == 2 {
		if secs, err := strconv.Atoi(m[1]); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second, true
		}
	}
	return 0, false
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
