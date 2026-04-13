package tunnel

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RetryConfig defines retry behavior for tunnel operations
type RetryConfig struct {
	MaxAttempts       int
	InitialDelay      time.Duration
	MaxDelay          time.Duration
	BackoffMultiplier float64
}

// DefaultRetryConfig returns sensible defaults for tunnel operations
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:       5,
		InitialDelay:      100 * time.Millisecond,
		MaxDelay:          5 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// isRetryableError determines if a gRPC error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check gRPC status codes
	st, ok := status.FromError(err)
	if !ok {
		// Not a gRPC error, retry for connection issues
		return true
	}

	// Retry on transient errors
	switch st.Code() {
	case codes.Unavailable, // Server unavailable
		codes.DeadlineExceeded,  // Timeout
		codes.Aborted,           // Operation aborted
		codes.ResourceExhausted: // Rate limited
		return true
	default:
		return false
	}
}

// retryWithBackoff executes an operation with exponential backoff retry
func retryWithBackoff(ctx context.Context, logger *zap.Logger, config RetryConfig, operation string, fn func() error) error {
	var lastErr error
	delay := config.InitialDelay

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		// Execute the operation
		err := fn()
		if err == nil {
			if attempt > 1 {
				logger.Info("Operation succeeded after retry",
					zap.String("operation", operation),
					zap.Int("attempt", attempt))
			}
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			logger.Debug("Non-retryable error, failing immediately",
				zap.String("operation", operation),
				zap.Error(err))
			return err
		}

		// Check if we've exhausted attempts
		if attempt >= config.MaxAttempts {
			logger.Error("Operation failed after max retries",
				zap.String("operation", operation),
				zap.Int("attempts", attempt),
				zap.Error(lastErr))
			return fmt.Errorf("operation failed after %d attempts: %w", attempt, lastErr)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		// Log retry attempt
		logger.Warn("Operation failed, retrying",
			zap.String("operation", operation),
			zap.Int("attempt", attempt),
			zap.Int("maxAttempts", config.MaxAttempts),
			zap.Duration("delay", delay),
			zap.Error(err))

		// Wait before retry
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled during backoff: %w", ctx.Err())
		}

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * config.BackoffMultiplier)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	return lastErr
}

// Made with Bob
