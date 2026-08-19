package fetcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// fetchBody télécharge une URL avec up to `retries` retries (backoff croissant),
// gestion gzip et borne de taille. Ne retente que sur erreur réseau / 5xx / 429.
func fetchBody(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	var lastErr error
	backoff := []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond}

	for attempt := 0; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept-Encoding", "gzip")
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("get %s: %w", rawURL, err)
			waitRetry(ctx, backoff, attempt)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			waitRetry(ctx, backoff, attempt)
			continue
		}

		// Retry sur 5xx et 429 (respère Retry-After quand présent) ; pas sur 4xx.
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("get %s: HTTP %d", rawURL, resp.StatusCode)
			var d time.Duration
			if attempt < len(backoff) {
				d = backoff[attempt]
			}
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, perr := time.ParseDuration(ra + "s"); perr == nil && secs > (d/3) {
					d = secs
				}
			}
			waitRetry(ctx, []time.Duration{d}, attempt)
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("get %s: HTTP %d", rawURL, resp.StatusCode)
		}

		body, err = gunzipIfNeeded(resp.Header, body)
		if err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, lastErr
}

// waitRetry attend entre les tentatives, en interrompant le ctx s'il est annulé.
func waitRetry(ctx context.Context, backoffs []time.Duration, attempt int) {
	if attempt >= len(backoffs) {
		return
	}
	t := time.NewTimer(backoffs[attempt])
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// gunzipIfNeeded décompresse si l'en-tête l'indique OU si les magic bytes
// gzip (1f 8b) sont présents. Ne décompresse jamais deux fois.
func gunzipIfNeeded(header http.Header, raw []byte) ([]byte, error) {
	isGzip := strings.EqualFold(header.Get("Content-Encoding"), "gzip")
	if !isGzip && len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		isGzip = true
	}
	if !isGzip {
		return raw, nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gzip header: %w", err)
	}
	defer gz.Close()
	return io.ReadAll(gz)
}
