// Package erpnext is the ERPNext v16 adapter's transport: a small Frappe REST
// client that always acts with the calling principal's own credential.
package erpnext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

const (
	defaultTimeout = 10 * time.Second
	maxBodyBytes   = 32 << 20
)

// getRetryDelays are the sleeps before the first and second GET retry.
var getRetryDelays = [...]time.Duration{200 * time.Millisecond, 800 * time.Millisecond}

// ClientConfig configures a Client. Timeout applies to each HTTP attempt
// (default 10 s); Transport defaults to http.DefaultTransport.
type ClientConfig struct {
	BaseURL, Site string
	Timeout       time.Duration
	Transport     http.RoundTripper
}

// Client talks Frappe REST. It is safe for concurrent use.
type Client struct {
	base    string
	site    string
	timeout time.Duration
	http    *http.Client
}

// NewClient builds a Client from cfg.
func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	rt := cfg.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &Client{
		base:    strings.TrimRight(cfg.BaseURL, "/"),
		site:    cfg.Site,
		timeout: timeout,
		http:    &http.Client{Transport: rt},
	}
}

// Call POSTs args as JSON to /api/method/<method> and decodes the unwrapped
// "message" into out (nil out discards it). Writes are never retried.
func (c *Client) Call(ctx context.Context, as erp.Principal, method string, args, out any) error {
	return c.post(ctx, as.Credential, as.OnBehalfOf, method, args, out)
}

// CallCred is Call for a bare credential (Whoami has no principal yet).
func (c *Client) CallCred(ctx context.Context, cred erp.Credential, method string, args, out any) error {
	return c.post(ctx, cred, "", method, args, out)
}

func (c *Client) post(ctx context.Context, cred erp.Credential, onBehalfOf, method string, args, out any) error {
	body, err := encodeBody(method, args)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, c.base+"/api/method/"+method, body, cred, onBehalfOf, unwrapped(out))
}

// Insert POSTs doc as JSON to /api/resource/<doctype> and decodes the created
// document ("data") into out (nil out discards it). Writes are never retried.
func (c *Client) Insert(ctx context.Context, as erp.Principal, doctype string, doc, out any) error {
	body, err := encodeBody(doctype, doc)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, c.base+ResourcePath(doctype, ""), body, as.Credential, as.OnBehalfOf, unwrapped(out))
}

// ResourcePath is /api/resource/<doctype>[/<name>] with both segments escaped.
func ResourcePath(doctype, name string) string {
	p := "/api/resource/" + url.PathEscape(doctype)
	if name != "" {
		p += "/" + url.PathEscape(name)
	}
	return p
}

func encodeBody(what string, v any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("erpnext: encode %s body: %w", what, err)
	}
	return b, nil
}

// Get issues GET path?query (path starts with /api/) and decodes the
// unwrapped "data" (or "message") into out. ErrUnavailable is retried twice
// (200 ms, then 800 ms).
func (c *Client) Get(ctx context.Context, as erp.Principal, path string, query url.Values, out any) error {
	return c.get(ctx, as, path, query, unwrapped(out))
}

// GetBody is Get for answers outside the data/message envelope (such as
// frappe.desk.form.load.getdoctype): the whole body is decoded into out.
func (c *Client) GetBody(ctx context.Context, as erp.Principal, path string, query url.Values, out any) error {
	return c.get(ctx, as, path, query, func(payload []byte) error {
		if out == nil {
			return nil
		}
		return decodeJSON(payload, out)
	})
}

func (c *Client) get(ctx context.Context, as erp.Principal, path string, query url.Values, decode func([]byte) error) error {
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var err error
	for attempt := 0; ; attempt++ {
		err = c.do(ctx, http.MethodGet, target, nil, as.Credential, as.OnBehalfOf, decode)
		if err == nil || !errors.Is(err, erp.ErrUnavailable) || attempt >= len(getRetryDelays) {
			return err
		}
		t := time.NewTimer(getRetryDelays[attempt])
		select {
		case <-ctx.Done():
			t.Stop()
			return fmt.Errorf("erpnext: GET %s: %w", path, ctx.Err())
		case <-t.C:
		}
	}
}

func (c *Client) do(ctx context.Context, method, target string, body []byte, cred erp.Credential, onBehalfOf string, decode func([]byte) error) error {
	if cred == nil {
		return fmt.Errorf("%w: no credential on principal", erp.ErrUnauthenticated)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(attemptCtx, method, target, rdr)
	if err != nil {
		return fmt.Errorf("erpnext: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Frappe-Site-Name", c.site)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch cr := cred.(type) {
	case erp.SessionCredential:
		req.Header.Set("Cookie", "sid="+cr.SessionID)
		req.Header.Set("X-Frappe-CSRF-Token", cr.CSRFToken)
	case erp.APIKeyCredential:
		req.Header.Set("Authorization", "token "+cr.Key+":"+cr.Secret)
	default:
		return fmt.Errorf("%w: unsupported credential %T", erp.ErrUnauthenticated, cred)
	}
	if onBehalfOf != "" {
		req.Header.Set("X-Town-On-Behalf-Of", onBehalfOf)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return c.transportError(ctx, attemptCtx, err)
	}
	defer func() { _ = res.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return c.transportError(ctx, attemptCtx, err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return mapError(res.StatusCode, payload)
	}
	return decode(payload)
}

func unwrapped(out any) func([]byte) error {
	return func(payload []byte) error { return decodeSuccess(payload, out) }
}

// transportError separates the caller's own cancellation from a backend that
// is unreachable or too slow (which includes this client's attempt timeout).
func (c *Client) transportError(parent, attempt context.Context, err error) error {
	if cerr := parent.Err(); cerr != nil {
		return fmt.Errorf("erpnext: request aborted: %w", cerr)
	}
	if attempt.Err() != nil {
		return fmt.Errorf("%w: timed out after %s", erp.ErrUnavailable, c.timeout)
	}
	return fmt.Errorf("%w: %w", erp.ErrUnavailable, err)
}

// decodeSuccess unwraps {"data": ...} or {"message": ...} into out, keeping
// JSON numbers exact (json.Number, never float64).
func decodeSuccess(payload []byte, out any) error {
	if out == nil {
		return nil
	}
	var env struct {
		Data    json.RawMessage `json:"data"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return fmt.Errorf("erpnext: decode response envelope: %w", err)
	}
	raw := env.Data
	if raw == nil {
		raw = env.Message
	}
	if raw == nil {
		return errors.New("erpnext: response has neither data nor message")
	}
	return decodeJSON(raw, out)
}

// decodeJSON decodes raw into out keeping numbers exact (json.Number).
func decodeJSON(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("erpnext: decode response: %w", err)
	}
	return nil
}
