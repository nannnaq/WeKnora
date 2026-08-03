package realtimecontext

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/utils"
)

const maxResponseBytes int64 = 8 << 20

type httpReader struct {
	client       *http.Client
	validateSSRF bool
}

func newHTTPReader(timeout time.Duration, override *http.Client) *httpReader {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if override != nil {
		clone := *override
		clone.Timeout = timeout
		clone.CheckRedirect = rejectRedirect
		return &httpReader{client: &clone}
	}
	client := utils.NewSSRFSafeHTTPClient(utils.SSRFSafeHTTPClientConfig{
		Timeout:      timeout,
		MaxRedirects: 0,
	})
	client.CheckRedirect = rejectRedirect
	return &httpReader{client: client, validateSSRF: true}
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func (h *httpReader) getJSON(ctx context.Context, baseURL, path string, query url.Values, target any) error {
	endpoint, err := buildURL(baseURL, path, query)
	if err != nil {
		return err
	}
	if h.validateSSRF {
		if err := utils.ValidateURLForSSRF(endpoint); err != nil {
			return fmt.Errorf("%s", utils.FormatSSRFError("realtime context endpoint", endpoint, err))
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build GET request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("GET failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET returned HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > maxResponseBytes {
		return fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode response JSON: %w", err)
	}
	return nil
}

func buildURL(baseURL, path string, query url.Values) (string, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("invalid realtime base URL")
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return "", fmt.Errorf("unsupported realtime URL scheme %q", base.Scheme)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path, "/")
	base.RawQuery = query.Encode()
	return base.String(), nil
}
