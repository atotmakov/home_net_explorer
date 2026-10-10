package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/atotmakov/home_net_explorer/internal/contract"
)

// ErrTokenRejected means the server refused the collector's token (401/403): retrying cannot
// help until the owner issues a new one.
var ErrTokenRejected = errors.New("upload: the server rejected the collector token")

// RejectedError is a run the server refused as invalid (400/413/422); it is not retried.
type RejectedError struct {
	Status int
	Code   string
	Detail string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("upload: server rejected the run (%d %s): %s", e.Status, e.Code, e.Detail)
}

// Client talks to the server's /api/v1 (contracts/collector-upload-api.yaml).
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	Log     *slog.Logger
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res, b, err
}

// Ping checks connectivity and the token, and returns the subnets the owner ignored.
func (c *Client) Ping(ctx context.Context) (contract.PingResponse, error) {
	var p contract.PingResponse
	res, body, err := c.do(ctx, http.MethodGet, "/api/v1/ping", nil)
	if err != nil {
		return p, err
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return p, ErrTokenRejected
	case res.StatusCode != http.StatusOK:
		return p, fmt.Errorf("upload: ping: unexpected status %s", res.Status)
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return p, fmt.Errorf("upload: ping: %w", err)
	}
	return p, nil
}

// RouterLogin fetches the login of router id configured in the web UI (feature 003). The caller
// keeps it in memory for one read; it is never logged.
func (c *Client) RouterLogin(ctx context.Context, id int64) (contract.RouterLogin, error) {
	var l contract.RouterLogin
	res, body, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/routers/%d/login", id), nil)
	if err != nil {
		return l, err
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return l, ErrTokenRejected
	case res.StatusCode != http.StatusOK:
		return l, fmt.Errorf("upload: router login: unexpected status %s", res.Status)
	}
	if err := json.Unmarshal(body, &l); err != nil {
		return l, errors.New("upload: router login: malformed answer") // never echo the body
	}
	return l, nil
}

// Upload sends one run body. Errors are ErrTokenRejected, *RejectedError, or a transient
// (network/5xx) error worth retrying.
func (c *Client) Upload(ctx context.Context, body []byte) (contract.UploadResult, error) {
	var r contract.UploadResult
	res, b, err := c.do(ctx, http.MethodPost, "/api/v1/collections", body)
	if err != nil {
		return r, err
	}
	switch res.StatusCode {
	case http.StatusCreated, http.StatusOK:
		err := json.Unmarshal(b, &r)
		return r, err
	case http.StatusUnauthorized, http.StatusForbidden:
		return r, ErrTokenRejected
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		var e contract.ErrorResponse
		json.Unmarshal(b, &e)
		return r, &RejectedError{Status: res.StatusCode, Code: e.Error, Detail: e.Detail}
	default:
		return r, fmt.Errorf("upload: unexpected status %s", res.Status)
	}
}

// FlushResult counts what a flush did.
type FlushResult struct {
	Uploaded int // stored or duplicate
	Rejected int // moved to spool/rejected/
	Last     contract.UploadResult
}

// Flush uploads every pending run, oldest first. It stops at the first token rejection
// (ErrTokenRejected, files kept) or transient error (returned, files kept for retry).
func (c *Client) Flush(ctx context.Context, sp Spool) (FlushResult, error) {
	var fr FlushResult
	items, err := sp.Pending()
	if err != nil {
		return fr, err
	}
	for _, it := range items {
		body, err := os.ReadFile(it.Path)
		if err != nil {
			return fr, err
		}
		r, err := c.Upload(ctx, body)
		var rej *RejectedError
		switch {
		case err == nil:
			fr.Uploaded++
			fr.Last = r
			if err := sp.Remove(it.ID); err != nil {
				return fr, err
			}
		case errors.As(err, &rej):
			fr.Rejected++
			c.log().Error("run rejected by the server; moved to spool/rejected", "collection_id", it.ID,
				"status", rej.Status, "error", rej.Code, "detail", rej.Detail)
			if err := sp.Reject(it.ID); err != nil {
				return fr, err
			}
		default:
			return fr, err
		}
	}
	return fr, nil
}

// Backoff is the retry schedule for transient upload failures: exponential from 1 minute up
// to 1 hour (research R8).
type Backoff struct {
	Min, Max time.Duration
	next     time.Duration
}

// NewBackoff returns the 1 minute → 1 hour schedule.
func NewBackoff() *Backoff { return &Backoff{Min: time.Minute, Max: time.Hour} }

// Next returns the next delay and doubles the following one.
func (b *Backoff) Next() time.Duration {
	if b.next == 0 {
		b.next = b.Min
	}
	d := b.next
	b.next *= 2
	if b.next > b.Max {
		b.next = b.Max
	}
	return d
}

// Reset starts over from Min after a success.
func (b *Backoff) Reset() { b.next = 0 }
