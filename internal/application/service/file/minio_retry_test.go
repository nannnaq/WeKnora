package file

import (
	"errors"
	"net"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestWrapMinioInitializationErrorMarksServerFailureTemporary(t *testing.T) {
	t.Parallel()

	serverErr := minio.ErrorResponse{
		Code:       "ServiceUnavailable",
		Message:    "please reduce your request rate",
		StatusCode: 503,
	}
	wantErr := error(serverErr)
	gotErr := wrapMinioInitializationError(wantErr)

	var netErr net.Error
	if !errors.As(gotErr, &netErr) {
		t.Fatalf("expected a temporary network error wrapper, got %T: %v", gotErr, gotErr)
	}
	if !netErr.Temporary() {
		t.Fatalf("expected HTTP 503 to be retryable, got %v", gotErr)
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("wrapped error must preserve the original MinIO error, got %v", gotErr)
	}
}

func TestWrapMinioInitializationErrorLeavesAuthenticationFailurePermanent(t *testing.T) {
	t.Parallel()

	authErr := minio.ErrorResponse{
		Code:       "AccessDenied",
		Message:    "access denied",
		StatusCode: 403,
	}
	wantErr := error(authErr)
	gotErr := wrapMinioInitializationError(wantErr)

	if gotErr != wantErr {
		t.Fatalf("authentication error must be returned unchanged, got %T: %v", gotErr, gotErr)
	}
	var netErr net.Error
	if errors.As(gotErr, &netErr) && netErr.Temporary() {
		t.Fatalf("authentication error must not be marked retryable: %v", gotErr)
	}
}
