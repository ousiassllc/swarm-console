package harness

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"
)

// Post は認証ヘッダーとして authz をそのまま付けて POST し、状態コードと本文を返す（authz が空なら付けない）。
func (h *H) Post(endpoint, authz, body string) (int, string) {
	h.T.Helper()
	status, resp, err := h.post(endpoint, authz, body)
	if err != nil {
		h.T.Fatalf("POST %s: %v", endpoint, err)
	}
	return status, resp
}

func (h *H) post(endpoint, authz, body string) (int, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+h.Addr+endpoint, strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.String(), nil
}
