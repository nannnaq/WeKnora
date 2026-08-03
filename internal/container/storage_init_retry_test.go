package container

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestInitMinioFileServiceWithRetryRecoversFromDNSFailure(t *testing.T) {
	t.Parallel()

	wantService := &struct{ name string }{name: "recovered"}
	attempts := 0
	var delays []time.Duration

	gotService, err := retryInitialization(
		context.Background(),
		func() (*struct{ name string }, error) {
			attempts++
			if attempts <= 2 {
				return nil, fmt.Errorf("failed to check bucket: %w", &url.Error{
					Op:  "Get",
					URL: "http://minio:9000/weknora/?location=",
					Err: &net.DNSError{Name: "minio", Err: "server misbehaving"},
				})
			}
			return wantService, nil
		},
		isTransientNetworkError,
		func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("expected MinIO initialization to recover, got %v", err)
	}
	if gotService != wantService {
		t.Fatal("expected the recovered file service to be returned")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 initialization attempts, got %d", attempts)
	}
	wantDelays := []time.Duration{time.Second, 2 * time.Second}
	if !reflect.DeepEqual(delays, wantDelays) {
		t.Fatalf("retry delays = %v, want %v", delays, wantDelays)
	}
}

func TestInitMinioFileServiceWithRetryReturnsPermanentFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("invalid MinIO credentials")
	attempts := 0
	waits := 0

	_, err := retryInitialization(
		context.Background(),
		func() (*struct{}, error) {
			attempts++
			return nil, wantErr
		},
		isTransientNetworkError,
		func(context.Context, time.Duration) error {
			waits++
			return nil
		},
		nil,
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if attempts != 1 {
		t.Fatalf("expected one initialization attempt, got %d", attempts)
	}
	if waits != 0 {
		t.Fatalf("permanent error must not wait, got %d waits", waits)
	}
}

func TestInitMinioFileServiceWithRetryCapsBackoffAtOneMinute(t *testing.T) {
	t.Parallel()

	attempts := 0
	var delays []time.Duration

	_, err := retryInitialization(
		context.Background(),
		func() (*struct{}, error) {
			attempts++
			if attempts <= 9 {
				return nil, &net.DNSError{Name: "minio", Err: "server misbehaving"}
			}
			return &struct{}{}, nil
		},
		isTransientNetworkError,
		func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("expected initialization to recover, got %v", err)
	}
	wantDelays := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		time.Minute,
		time.Minute,
		time.Minute,
	}
	if !reflect.DeepEqual(delays, wantDelays) {
		t.Fatalf("retry delays = %v, want %v", delays, wantDelays)
	}
}

func TestInitMinioFileServiceWithRetryStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0

	_, err := retryInitialization(
		ctx,
		func() (*struct{}, error) {
			attempts++
			return nil, &net.DNSError{Name: "minio", Err: "server misbehaving"}
		},
		isTransientNetworkError,
		waitForInitializationRetry,
		nil,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("expected one initialization attempt before cancellation, got %d", attempts)
	}
}
