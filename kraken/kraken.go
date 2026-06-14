// Package kraken is the library behind the kraken command line:
// the HTTP client, request shaping, and the typed data models for the
// Kraken crypto exchange public API (api.kraken.com/0/public).
//
// The Client is the spine every command shares. It sets a real User-Agent,
// paces requests so a busy session stays polite, and retries transient
// failures (429 and 5xx). Build your endpoint calls and JSON decoding on
// top of Get.
package kraken

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent identifies the client to Kraken.
const DefaultUserAgent = "kraken/dev (+https://github.com/tamnd/kraken-cli)"

// Host is the Kraken public API host.
const Host = "api.kraken.com"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host + "/0/public"

// Config holds optional overrides a caller can apply to a new Client.
type Config struct {
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Client talks to Kraken over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int
	// Base overrides BaseURL for testing. Leave empty to use BaseURL.
	Base string

	last time.Time
}

// NewClient returns a Client with sensible defaults.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return BaseURL
}

// Get fetches url and returns the response body. It paces and retries
// according to the client's settings.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- output types ---

// Ticker holds the current price summary for an asset pair.
type Ticker struct {
	Pair    string  `kit:"id" json:"pair"`
	Price   string  `json:"price"`    // c[0] = last trade price
	High24h string  `json:"high_24h"` // h[1]
	Low24h  string  `json:"low_24h"`  // l[1]
	Volume  string  `json:"volume"`   // v[1]
	Trades  float64 `json:"trades"`   // t[1]
}

// Asset describes a single Kraken asset (currency).
type Asset struct {
	ID       string `kit:"id" json:"id"`
	AltName  string `json:"alt_name"`
	Decimals int    `json:"decimals"`
	Status   string `json:"status"`
}

// Pair describes a Kraken tradeable asset pair.
type Pair struct {
	ID      string `kit:"id" json:"id"`
	AltName string `json:"alt_name"`
	Base    string `json:"base"`
	Quote   string `json:"quote"`
	Status  string `json:"status"`
}

// Candle is one OHLC bar.
type Candle struct {
	Pair   string `kit:"id" json:"pair"`
	Time   int64  `json:"time"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	VWAP   string `json:"vwap"`
	Volume string `json:"volume"`
	Count  int    `json:"count"`
}

// --- API response helpers ---

// krakenResp is the envelope every Kraken public endpoint returns.
type krakenResp struct {
	Error  []string        `json:"error"`
	Result json.RawMessage `json:"result"`
}

// decode unmarshals a Kraken response envelope from body. If the API
// returned error strings, the first one is returned as an error.
func decode(body []byte) (json.RawMessage, error) {
	var r krakenResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(r.Error) > 0 {
		return nil, fmt.Errorf("kraken API error: %s", r.Error[0])
	}
	return r.Result, nil
}

// --- Ticker ---

// GetTicker fetches the current ticker for a pair (e.g. "XBTUSD").
func (c *Client) GetTicker(ctx context.Context, pair string) (*Ticker, error) {
	url := c.base() + "/Ticker?pair=" + pair
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	result, err := decode(body)
	if err != nil {
		return nil, err
	}

	// result is a map keyed by the internal pair name e.g. "XXBTZUSD"
	var m map[string]struct {
		C []string  `json:"c"` // [price, lot]
		H []string  `json:"h"` // [today, 24h]
		L []string  `json:"l"` // [today, 24h]
		V []string  `json:"v"` // [today, 24h]
		T []float64 `json:"t"` // [today, 24h]
	}
	if err := json.Unmarshal(result, &m); err != nil {
		return nil, fmt.Errorf("parse ticker: %w", err)
	}

	for key, v := range m {
		t := &Ticker{Pair: key}
		if len(v.C) > 0 {
			t.Price = v.C[0]
		}
		if len(v.H) > 1 {
			t.High24h = v.H[1]
		}
		if len(v.L) > 1 {
			t.Low24h = v.L[1]
		}
		if len(v.V) > 1 {
			t.Volume = v.V[1]
		}
		if len(v.T) > 1 {
			t.Trades = v.T[1]
		}
		return t, nil
	}
	return nil, fmt.Errorf("ticker: empty result")
}

// --- Assets ---

// GetAssets fetches all assets, optionally limited to limit items (0 = all).
func (c *Client) GetAssets(ctx context.Context, limit int) ([]*Asset, error) {
	url := c.base() + "/Assets"
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	result, err := decode(body)
	if err != nil {
		return nil, err
	}

	var m map[string]struct {
		AltName  string `json:"altname"`
		Decimals int    `json:"decimals"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(result, &m); err != nil {
		return nil, fmt.Errorf("parse assets: %w", err)
	}

	var out []*Asset
	for key, v := range m {
		out = append(out, &Asset{
			ID:       key,
			AltName:  v.AltName,
			Decimals: v.Decimals,
			Status:   v.Status,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// --- Pairs ---

// GetPairs fetches asset pairs. If pairs is non-empty it is passed as the
// "pair" query parameter (comma-separated). Optionally limited to limit items.
func (c *Client) GetPairs(ctx context.Context, pairs string, limit int) ([]*Pair, error) {
	u := c.base() + "/AssetPairs"
	if pairs != "" {
		u += "?pair=" + pairs
	}
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	result, err := decode(body)
	if err != nil {
		return nil, err
	}

	var m map[string]struct {
		AltName string `json:"altname"`
		Base    string `json:"base"`
		Quote   string `json:"quote"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(result, &m); err != nil {
		return nil, fmt.Errorf("parse pairs: %w", err)
	}

	var out []*Pair
	for key, v := range m {
		out = append(out, &Pair{
			ID:      key,
			AltName: v.AltName,
			Base:    v.Base,
			Quote:   v.Quote,
			Status:  v.Status,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// --- OHLC ---

// GetOHLC fetches OHLC candles for a pair. interval is in minutes
// (1|5|15|60|1440). limit caps the number of returned candles (0 = all).
func (c *Client) GetOHLC(ctx context.Context, pair string, interval, limit int) ([]*Candle, error) {
	if interval <= 0 {
		interval = 1440
	}
	u := fmt.Sprintf("%s/OHLC?pair=%s&interval=%d", c.base(), pair, interval)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	result, err := decode(body)
	if err != nil {
		return nil, err
	}

	// result is a map: {"XXBTZUSD": [[...], ...], "last": N}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(result, &raw); err != nil {
		return nil, fmt.Errorf("parse ohlc map: %w", err)
	}

	var pairKey string
	var rowsRaw json.RawMessage
	for k, v := range raw {
		if k == "last" {
			continue
		}
		pairKey = k
		rowsRaw = v
		break
	}
	if pairKey == "" {
		return nil, fmt.Errorf("ohlc: no pair key in result")
	}

	var rows [][]interface{}
	if err := json.Unmarshal(rowsRaw, &rows); err != nil {
		return nil, fmt.Errorf("parse ohlc rows: %w", err)
	}

	var out []*Candle
	for _, row := range rows {
		if len(row) < 8 {
			continue
		}
		candle := &Candle{Pair: pairKey}
		if v, ok := row[0].(float64); ok {
			candle.Time = int64(v)
		}
		if v, ok := row[1].(string); ok {
			candle.Open = v
		}
		if v, ok := row[2].(string); ok {
			candle.High = v
		}
		if v, ok := row[3].(string); ok {
			candle.Low = v
		}
		if v, ok := row[4].(string); ok {
			candle.Close = v
		}
		if v, ok := row[5].(string); ok {
			candle.VWAP = v
		}
		if v, ok := row[6].(string); ok {
			candle.Volume = v
		}
		if v, ok := row[7].(float64); ok {
			candle.Count = int(v)
		}
		out = append(out, candle)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
