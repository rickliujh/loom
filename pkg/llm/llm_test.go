package llm

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// mockModel implements llms.Model for testing.
type mockModel struct {
	calls    atomic.Int32
	handler  func(attempt int) (*llms.ContentResponse, error)
}

func (m *mockModel) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	attempt := int(m.calls.Add(1))
	return m.handler(attempt)
}

func (m *mockModel) Call(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
	return "", errors.New("not implemented")
}

func successResponse(content string) *llms.ContentResponse {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: content}},
	}
}

func emptyResponse() *llms.ContentResponse {
	return &llms.ContentResponse{}
}

func TestInferWithModel_NoRetry_Success(t *testing.T) {
	m := &mockModel{handler: func(_ int) (*llms.ContentResponse, error) {
		return successResponse("hello"), nil
	}}

	result, err := inferWithModel(context.Background(), m, InferenceOptions{
		Provider: "test",
		Prompt:   "hi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Fatalf("got %q, want %q", result, "hello")
	}
	if m.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", m.calls.Load())
	}
}

func TestInferWithModel_NoRetry_Error(t *testing.T) {
	m := &mockModel{handler: func(_ int) (*llms.ContentResponse, error) {
		return nil, errors.New("api error")
	}}

	_, err := inferWithModel(context.Background(), m, InferenceOptions{
		Provider: "test",
		Prompt:   "hi",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if m.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", m.calls.Load())
	}
}

func TestInferWithModel_RetryThenSuccess(t *testing.T) {
	m := &mockModel{handler: func(attempt int) (*llms.ContentResponse, error) {
		if attempt <= 2 {
			return nil, errors.New("transient error")
		}
		return successResponse("recovered"), nil
	}}

	result, err := inferWithModel(context.Background(), m, InferenceOptions{
		Provider:   "test",
		Prompt:     "hi",
		Retries:    3,
		RetryDelay: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "recovered" {
		t.Fatalf("got %q, want %q", result, "recovered")
	}
	if m.calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", m.calls.Load())
	}
}

func TestInferWithModel_RetryExhausted(t *testing.T) {
	m := &mockModel{handler: func(_ int) (*llms.ContentResponse, error) {
		return nil, errors.New("persistent error")
	}}

	_, err := inferWithModel(context.Background(), m, InferenceOptions{
		Provider:   "test",
		Prompt:     "hi",
		Retries:    2,
		RetryDelay: 1 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if m.calls.Load() != 3 {
		t.Fatalf("expected 3 calls (1 + 2 retries), got %d", m.calls.Load())
	}
}

func TestInferWithModel_RetryEmptyResponse(t *testing.T) {
	m := &mockModel{handler: func(attempt int) (*llms.ContentResponse, error) {
		if attempt == 1 {
			return emptyResponse(), nil
		}
		return successResponse("got it"), nil
	}}

	result, err := inferWithModel(context.Background(), m, InferenceOptions{
		Provider:   "test",
		Prompt:     "hi",
		Retries:    1,
		RetryDelay: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "got it" {
		t.Fatalf("got %q, want %q", result, "got it")
	}
	if m.calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", m.calls.Load())
	}
}

func TestInferWithModel_RetryContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &mockModel{handler: func(_ int) (*llms.ContentResponse, error) {
		cancel() // cancel during first failure so retry wait is interrupted
		return nil, errors.New("fail")
	}}

	_, err := inferWithModel(ctx, m, InferenceOptions{
		Provider:   "test",
		Prompt:     "hi",
		Retries:    3,
		RetryDelay: 10 * time.Second, // long delay to ensure context cancellation is what stops it
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestInferWithModel_NoRetry_EmptyResponseFails(t *testing.T) {
	m := &mockModel{handler: func(_ int) (*llms.ContentResponse, error) {
		return emptyResponse(), nil
	}}

	_, err := inferWithModel(context.Background(), m, InferenceOptions{
		Provider: "test",
		Prompt:   "hi",
	})
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if m.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", m.calls.Load())
	}
}

func TestInferWithModel_DefaultRetryDelay(t *testing.T) {
	start := time.Now()
	m := &mockModel{handler: func(attempt int) (*llms.ContentResponse, error) {
		if attempt == 1 {
			return nil, errors.New("fail")
		}
		return successResponse("ok"), nil
	}}

	_, err := inferWithModel(context.Background(), m, InferenceOptions{
		Provider:   "test",
		Prompt:     "hi",
		Retries:    1,
		RetryDelay: 0, // should default to 2s
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 2*time.Second {
		t.Fatalf("expected at least 2s delay (default), got %v", elapsed)
	}
}

func TestInferWithModel_ExponentialBackoff(t *testing.T) {
	var timestamps []time.Time
	m := &mockModel{handler: func(attempt int) (*llms.ContentResponse, error) {
		timestamps = append(timestamps, time.Now())
		if attempt <= 2 {
			return nil, errors.New("fail")
		}
		return successResponse("ok"), nil
	}}

	_, err := inferWithModel(context.Background(), m, InferenceOptions{
		Provider:   "test",
		Prompt:     "hi",
		Retries:    2,
		RetryDelay: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(timestamps) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(timestamps))
	}

	// First retry: ~50ms delay
	gap1 := timestamps[1].Sub(timestamps[0])
	if gap1 < 40*time.Millisecond {
		t.Errorf("first retry gap too short: %v", gap1)
	}
	// Second retry: ~100ms delay (doubled)
	gap2 := timestamps[2].Sub(timestamps[1])
	if gap2 < 80*time.Millisecond {
		t.Errorf("second retry gap too short (expected ~100ms): %v", gap2)
	}
}
