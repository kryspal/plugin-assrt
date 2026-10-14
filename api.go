package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/kryspal/plugin-sdk/sdk"
)

// The ASSRT API (https://api.assrt.net, documented at assrt.net/api/doc): GET {base}/sub/search and {base}/sub/detail, the token in the
// Authorization: Bearer header (never in the URL). Every response is JSON with a "status" (0 is success), also on HTTP errors:
//
//	1      no such user                         20001  token does not exist
//	101    the keyword is too short             20000  a parameter is missing
//	20900  no such subtitle                     30900  over the request quota (20 per minute per token and IP)
//	30000–30300  server errors
//
// The terms: free for personal use, credit the site, never keep download URLs (each is unique and expires).

const (
	maxResponseBytes = 2 << 20
	requestTimeout   = 20 * time.Second
	// The quota is 20 requests a minute per token and IP; the plugin stays below it and leaves room for the host's own retries.
	quotaPerMinute = 18
	// minInterval is the minimum interval between two API requests.
	minInterval = 400 * time.Millisecond
	// defaultRetryAfter is the pause time when ASSRT says the quota is exceeded.
	defaultRetryAfter = time.Minute
)

// Statuses from ASSRT that the plugin acts on.
const (
	statusOK           = 0
	statusNoUser       = 1
	statusKeywordShort = 101
	statusBadToken     = 20001
	statusNoSuchSub    = 20900
	statusOverQuota    = 30900
)

var (
	errUnauthorized = errors.New("ASSRT rejected the token")
	errNoSuchSub    = errors.New("no such subtitle")
	errKeywordShort = errors.New("the keyword is too short")
)

// api is the ASSRT HTTP client: one request at a time, at least minInterval apart, and at most quotaPerMinute API requests in any minute
// (past that it returns a rate limit without sending anything, so the host pauses the plugin instead of ASSRT blocking the token).
type api struct {
	http *http.Client
	mu   sync.Mutex
	last time.Time
	sent []time.Time // API requests in the last minute
	// interval and window can be changed in tests.
	interval time.Duration
	perMin   int
}

func newAPI() *api {
	return &api{http: &http.Client{Timeout: requestTimeout}, interval: minInterval, perMin: quotaPerMinute}
}

// throttle waits for the spacing and reserves a place in the minute window; callers hold a.mu.
func (a *api) throttle(ctx context.Context) error {
	if d := time.Until(a.last.Add(a.interval)); d > 0 {
		t := time.NewTimer(d)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
	now := time.Now()
	keep := a.sent[:0]
	for _, ts := range a.sent {
		if now.Sub(ts) < time.Minute {
			keep = append(keep, ts)
		}
	}
	a.sent = keep
	if len(a.sent) >= a.perMin {
		return sdk.RateLimited(time.Minute-now.Sub(a.sent[0]), "assrt: request quota reached")
	}
	a.sent = append(a.sent, now)
	a.last = now
	return nil
}

type envelope struct {
	Status int `json:"status"`
}

// call sends one API request and decodes the JSON body into out. ASSRT statuses map to errors: a rejected token errUnauthorized, a missing
// subtitle errNoSuchSub, a short keyword errKeywordShort, over quota a rate limit, server statuses and connection errors Unavailable.
func (a *api) call(ctx context.Context, token, rawURL string, out any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.throttle(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return sdk.Unavailable("assrt: bad request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "KryspalMedia-assrt/"+version())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.http.Do(req)
	if err != nil {
		return transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return sdk.Unavailable("assrt: " + err.Error())
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return sdk.RateLimited(retryAfter(resp), "assrt: rate limit reached")
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return sdk.Unavailable("assrt: " + resp.Status)
	}
	switch env.Status {
	case statusOK:
		if resp.StatusCode != http.StatusOK {
			return sdk.Unavailable("assrt: " + resp.Status)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return sdk.Unavailable("assrt: bad response")
		}
		return nil
	case statusNoUser, statusBadToken:
		return errUnauthorized
	case statusKeywordShort:
		return errKeywordShort
	case statusNoSuchSub:
		return errNoSuchSub
	case statusOverQuota:
		return sdk.RateLimited(retryAfter(resp), "assrt: request quota exceeded")
	}
	if env.Status >= 30000 || resp.StatusCode >= 500 {
		return sdk.Unavailable("assrt: server error " + strconv.Itoa(env.Status))
	}
	return sdk.InvalidArgument("assrt: request refused with status " + strconv.Itoa(env.Status))
}

func retryAfter(resp *http.Response) time.Duration {
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return defaultRetryAfter
}

func transportError(ctx context.Context, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err // url.Error carries the full URL; keep only the cause
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return sdk.Unavailable("assrt: " + err.Error())
}

// fetch downloads one subtitle file from a signed URL (no token is sent: the URL is not an API call). It returns nil when the file is larger than max.
func (a *api) fetch(ctx context.Context, rawURL string, max int) ([]byte, error) {
	// ASSRT sends http:// links; the same host serves https, so the file is always fetched over https.
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, nil
	}
	u.Scheme = "https"
	rawURL = u.String()
	a.mu.Lock()
	if d := time.Until(a.last.Add(a.interval)); d > 0 {
		t := time.NewTimer(d)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			a.mu.Unlock()
			return nil, ctx.Err()
		}
	}
	a.last = time.Now()
	a.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, sdk.Unavailable("assrt: bad request")
	}
	req.Header.Set("User-Agent", "KryspalMedia-assrt/"+version())
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 500 {
		return nil, sdk.Unavailable("assrt: " + resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(max)+1))
	if err != nil {
		return nil, sdk.Unavailable("assrt: " + err.Error())
	}
	if len(data) > max {
		return nil, nil
	}
	return data, nil
}
