package api

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func fastRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		Multiplier:     2.0,
		Jitter:         0,
	}
}

func TestWithRetry_SuccessFirstAttempt(t *testing.T) {
	calls := 0
	result, err := WithRetry(context.Background(), fastRetryConfig(), func() (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestWithRetry_SuccessAfterRetry(t *testing.T) {
	calls := 0
	rateLimitErr := &APIError{
		Type: "error",
		ErrorDetails: ErrorDetail{
			Type:    ErrorTypeRateLimit,
			Message: "rate limited",
		},
		StatusCode: 429,
	}

	result, err := WithRetry(context.Background(), fastRetryConfig(), func() (string, error) {
		calls++
		if calls < 3 {
			return "", rateLimitErr
		}
		return "recovered", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "recovered" {
		t.Errorf("result = %q, want %q", result, "recovered")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestWithRetry_NonRetryableError(t *testing.T) {
	calls := 0
	authErr := &APIError{
		Type: "error",
		ErrorDetails: ErrorDetail{
			Type:    ErrorTypeAuthentication,
			Message: "invalid api key",
		},
		StatusCode: 401,
	}

	_, err := WithRetry(context.Background(), fastRetryConfig(), func() (string, error) {
		calls++
		return "", authErr
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retries for auth error)", calls)
	}

	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected *RetryableError, got %T", err)
	}
	if retryErr.Info.TotalAttempts != 1 {
		t.Errorf("TotalAttempts = %d, want 1", retryErr.Info.TotalAttempts)
	}
	if retryErr.Reason != "non-retryable error" {
		t.Errorf("Reason = %q, want %q", retryErr.Reason, "non-retryable error")
	}
}

func TestWithRetry_MaxRetriesExhausted(t *testing.T) {
	calls := 0
	rateLimitErr := &APIError{
		Type: "error",
		ErrorDetails: ErrorDetail{
			Type:    ErrorTypeRateLimit,
			Message: "rate limited",
		},
		StatusCode: 429,
	}

	_, err := WithRetry(context.Background(), fastRetryConfig(), func() (string, error) {
		calls++
		return "", rateLimitErr
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 1 initial + 3 retries = 4 total attempts
	if calls != 4 {
		t.Errorf("calls = %d, want 4", calls)
	}

	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected *RetryableError, got %T", err)
	}
	if retryErr.Info.TotalAttempts != 4 {
		t.Errorf("TotalAttempts = %d, want 4", retryErr.Info.TotalAttempts)
	}
	if !retryErr.Info.Retried {
		t.Error("Info.Retried = false, want true")
	}
	if retryErr.Reason != "max retries exceeded" {
		t.Errorf("Reason = %q, want %q", retryErr.Reason, "max retries exceeded")
	}
}

func TestWithRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	rateLimitErr := &APIError{
		Type: "error",
		ErrorDetails: ErrorDetail{
			Type:    ErrorTypeRateLimit,
			Message: "rate limited",
		},
		StatusCode: 429,
	}

	cfg := RetryConfig{
		MaxRetries:     5,
		InitialBackoff: 500 * time.Millisecond, // long enough to cancel during
		MaxBackoff:     time.Second,
		Multiplier:     2.0,
		Jitter:         0,
	}

	// Cancel after the first failure, during backoff
	cfg.OnRetry = func(attempt int, err error, backoff time.Duration) {
		cancel()
	}

	_, err := WithRetry(ctx, cfg, func() (string, error) {
		calls++
		return "", rateLimitErr
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected *RetryableError, got %T", err)
	}
	if !errors.Is(retryErr.Err, context.Canceled) {
		t.Errorf("underlying error = %v, want context.Canceled", retryErr.Err)
	}
	if retryErr.Reason != "context cancelled during backoff" {
		t.Errorf("Reason = %q, want %q", retryErr.Reason, "context cancelled during backoff")
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "RateLimit",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeRateLimit, Message: "rate limited"},
				StatusCode:   429,
			},
			want: true,
		},
		{
			name: "Overloaded",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeOverloaded, Message: "overloaded"},
				StatusCode:   529,
			},
			want: true,
		},
		{
			name: "AuthError",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeAuthentication, Message: "invalid key"},
				StatusCode:   401,
			},
			want: false,
		},
		{
			name: "NetworkError",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &net.DNSError{
					Err:         "lookup failed",
					Name:        "api.anthropic.com",
					IsTemporary: true,
				},
			},
			want: true,
		},
		{
			name: "ContextCanceled",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "ContextDeadlineExceeded",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "InvalidRequest",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeInvalidRequest, Message: "bad request"},
				StatusCode:   400,
			},
			want: false,
		},
		{
			name: "PermissionError",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypePermission, Message: "forbidden"},
				StatusCode:   403,
			},
			want: false,
		},
		{
			name: "NotFoundError",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeNotFound, Message: "not found"},
				StatusCode:   404,
			},
			want: false,
		},
		{
			name: "NilError",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRetry(tt.err)
			if got != tt.want {
				t.Errorf("ShouldRetry(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestCalculateBackoff_RespectsRetryAfter(t *testing.T) {
	cfg := fastRetryConfig()
	retryAfter := 5 * time.Second

	err := &APIError{
		Type:         "error",
		ErrorDetails: ErrorDetail{Type: ErrorTypeRateLimit, Message: "rate limited"},
		StatusCode:   429,
		RetryAfter:   retryAfter,
	}

	got := calculateBackoff(cfg, time.Millisecond, err)
	if got != retryAfter {
		t.Errorf("calculateBackoff = %v, want %v (should respect Retry-After)", got, retryAfter)
	}
}

func TestCalculateBackoff_NoJitter(t *testing.T) {
	cfg := fastRetryConfig()
	cfg.Jitter = 0

	normalErr := &APIError{
		Type:         "error",
		ErrorDetails: ErrorDetail{Type: ErrorTypeRateLimit, Message: "rate limited"},
		StatusCode:   429,
	}

	backoff := 5 * time.Millisecond
	got := calculateBackoff(cfg, backoff, normalErr)
	if got != backoff {
		t.Errorf("calculateBackoff = %v, want %v (no jitter)", got, backoff)
	}
}

func TestRetryableError_Error(t *testing.T) {
	t.Run("WithRetry", func(t *testing.T) {
		err := &RetryableError{
			Err:    errors.New("rate limited"),
			Info:   RetryInfo{TotalAttempts: 3, Retried: true},
			Reason: "max retries exceeded",
		}
		want := "rate limited (after 3 attempts)"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})

	t.Run("WithoutRetry", func(t *testing.T) {
		err := &RetryableError{
			Err:    errors.New("auth error"),
			Info:   RetryInfo{TotalAttempts: 1, Retried: false},
			Reason: "non-retryable error",
		}
		want := "auth error"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})
}

func TestRetryableError_Unwrap(t *testing.T) {
	inner := errors.New("inner error")
	err := &RetryableError{
		Err:  inner,
		Info: RetryInfo{TotalAttempts: 1},
	}
	if !errors.Is(err, inner) {
		t.Error("Unwrap should allow errors.Is to find inner error")
	}
}

func TestExtractRetryInfo(t *testing.T) {
	t.Run("WithRetryableError", func(t *testing.T) {
		err := &RetryableError{
			Err:  errors.New("fail"),
			Info: RetryInfo{TotalAttempts: 3, Retried: true},
		}
		info := ExtractRetryInfo(err)
		if info == nil {
			t.Fatal("expected non-nil RetryInfo")
		}
		if info.TotalAttempts != 3 {
			t.Errorf("TotalAttempts = %d, want 3", info.TotalAttempts)
		}
	})

	t.Run("WithOtherError", func(t *testing.T) {
		info := ExtractRetryInfo(errors.New("plain error"))
		if info != nil {
			t.Errorf("expected nil RetryInfo, got %+v", info)
		}
	})
}

func TestIsRetryableHTTPStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{429, true},
		{529, true},
		{502, true},
		{503, true},
		{504, true},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{200, false},
	}

	for _, tt := range tests {
		t.Run("status_"+string(rune('0'+tt.code/100))+string(rune('0'+(tt.code%100)/10))+string(rune('0'+tt.code%10)), func(t *testing.T) {
			got := IsRetryableHTTPStatus(tt.code)
			if got != tt.want {
				t.Errorf("IsRetryableHTTPStatus(%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestErrorClassifiers(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		fn       func(error) bool
		wantTrue bool
	}{
		{
			name: "IsRateLimitError_true",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeRateLimit, Message: "rate limited"},
			},
			fn:       IsRateLimitError,
			wantTrue: true,
		},
		{
			name:     "IsRateLimitError_false",
			err:      errors.New("other"),
			fn:       IsRateLimitError,
			wantTrue: false,
		},
		{
			name: "IsOverloadedError_true",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeOverloaded, Message: "overloaded"},
			},
			fn:       IsOverloadedError,
			wantTrue: true,
		},
		{
			name: "IsAuthenticationError_true",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeAuthentication, Message: "bad key"},
			},
			fn:       IsAuthenticationError,
			wantTrue: true,
		},
		{
			name: "IsPermissionError_true",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypePermission, Message: "forbidden"},
			},
			fn:       IsPermissionError,
			wantTrue: true,
		},
		{
			name: "IsInvalidRequestError_true",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeInvalidRequest, Message: "bad req"},
			},
			fn:       IsInvalidRequestError,
			wantTrue: true,
		},
		{
			name: "IsNotFoundError_true",
			err: &APIError{
				Type:         "error",
				ErrorDetails: ErrorDetail{Type: ErrorTypeNotFound, Message: "missing"},
			},
			fn:       IsNotFoundError,
			wantTrue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(tt.err)
			if got != tt.wantTrue {
				t.Errorf("%s = %v, want %v", tt.name, got, tt.wantTrue)
			}
		})
	}
}
