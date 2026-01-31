package dispatcher

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type HTTPWebhookSender struct {
	client *http.Client
}

func NewHTTPWebhookSender() *HTTPWebhookSender {
	return &HTTPWebhookSender{
		client: &http.Client{},
	}
}

// Send posts the webhook payload with HMAC signature.
// Headers: X-EasyCron-Event-ID (attempt), X-EasyCron-Execution-ID, X-EasyCron-Signature
func (s *HTTPWebhookSender) Send(ctx context.Context, req WebhookRequest) WebhookResult {
	start := time.Now()

	body, err := json.Marshal(req.Payload)
	if err != nil {
		return WebhookResult{Error: fmt.Errorf("marshal: %w", err), Duration: time.Since(start)}
	}

	signature := computeSignature(req.Secret, body)

	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctxTimeout, http.MethodPost, req.URL, bytes.NewReader(body))
	if err != nil {
		return WebhookResult{Error: fmt.Errorf("create request: %w", err), Duration: time.Since(start)}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-EasyCron-Event-ID", req.AttemptID)
	httpReq.Header.Set("X-EasyCron-Execution-ID", req.Payload.ExecutionID)
	httpReq.Header.Set("X-EasyCron-Signature", signature)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return WebhookResult{Error: fmt.Errorf("send: %w", err), Duration: time.Since(start)}
	}
	defer resp.Body.Close()

	return WebhookResult{StatusCode: resp.StatusCode, Duration: time.Since(start)}
}

func computeSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature is for customers to verify incoming webhooks.
func VerifySignature(secret string, body []byte, signature string) bool {
	expected := computeSignature(secret, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}
