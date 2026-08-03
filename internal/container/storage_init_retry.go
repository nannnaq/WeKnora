package container

import (
	"context"
	"errors"
	"net"
	"net/url"
	"syscall"
	"time"
)

var initializationRetryDelays = [...]time.Duration{
	time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	30 * time.Second,
	time.Minute,
}

type initializationRetryObserver func(attempt int, delay time.Duration, err error)

func retryInitialization[T any](
	ctx context.Context,
	initialize func() (T, error),
	retryable func(error) bool,
	wait func(context.Context, time.Duration) error,
	onRetry initializationRetryObserver,
) (T, error) {
	var zero T
	for attempt := 1; ; attempt++ {
		value, err := initialize()
		if err == nil {
			return value, nil
		}
		if !retryable(err) {
			return zero, err
		}

		delay := initializationRetryDelay(attempt)
		if onRetry != nil {
			onRetry(attempt, delay, err)
		}
		if err := wait(ctx, delay); err != nil {
			return zero, err
		}
	}
}

func initializationRetryDelay(attempt int) time.Duration {
	index := attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(initializationRetryDelays) {
		index = len(initializationRetryDelays) - 1
	}
	return initializationRetryDelays[index]
}

func isTransientNetworkError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isTransientNetworkError(urlErr.Err)
	}
	var netErr net.Error
	return errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary())
}

func waitForInitializationRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
