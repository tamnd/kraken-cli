package kraken_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/kraken-cli/kraken"
)

// --- Client transport tests ---

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := kraken.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := kraken.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGetFailsOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := kraken.NewClient()
	c.Rate = 0
	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error on 404")
	}
}

// --- helpers ---

// fakeServer returns a test server that always replies with v marshalled as JSON.
func fakeServer(t *testing.T, v interface{}) *httptest.Server {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fake response: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
}

// newTestClient returns a Client pointed at the given test server.
func newTestClient(srv *httptest.Server) *kraken.Client {
	c := kraken.NewClient()
	c.Rate = 0
	c.Base = srv.URL
	return c
}

// --- Ticker ---

func TestGetTicker(t *testing.T) {
	resp := map[string]interface{}{
		"error": []string{},
		"result": map[string]interface{}{
			"XXBTZUSD": map[string]interface{}{
				"c": []string{"65000.10", "0.001"},
				"h": []string{"65500.00", "66000.00"},
				"l": []string{"64000.00", "63500.00"},
				"v": []string{"1234.56", "9876.54"},
				"t": []float64{500, 4200},
			},
		},
	}
	srv := fakeServer(t, resp)
	defer srv.Close()

	c := newTestClient(srv)
	ticker, err := c.GetTicker(context.Background(), "XBTUSD")
	if err != nil {
		t.Fatal(err)
	}
	if ticker.Pair != "XXBTZUSD" {
		t.Errorf("Pair = %q, want XXBTZUSD", ticker.Pair)
	}
	if ticker.Price != "65000.10" {
		t.Errorf("Price = %q, want 65000.10", ticker.Price)
	}
	if ticker.High24h != "66000.00" {
		t.Errorf("High24h = %q, want 66000.00", ticker.High24h)
	}
	if ticker.Low24h != "63500.00" {
		t.Errorf("Low24h = %q, want 63500.00", ticker.Low24h)
	}
	if ticker.Volume != "9876.54" {
		t.Errorf("Volume = %q, want 9876.54", ticker.Volume)
	}
	if ticker.Trades != 4200 {
		t.Errorf("Trades = %v, want 4200", ticker.Trades)
	}
}

func TestGetTickerAPIError(t *testing.T) {
	resp := map[string]interface{}{
		"error":  []string{"EQuery:Unknown asset pair"},
		"result": map[string]interface{}{},
	}
	srv := fakeServer(t, resp)
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.GetTicker(context.Background(), "BADINPUT")
	if err == nil {
		t.Fatal("expected error from API error response")
	}
}

// --- Assets ---

func TestGetAssets(t *testing.T) {
	resp := map[string]interface{}{
		"error": []string{},
		"result": map[string]interface{}{
			"XXBT": map[string]interface{}{
				"altname":  "XBT",
				"decimals": 10,
				"status":   "enabled",
			},
			"XETH": map[string]interface{}{
				"altname":  "ETH",
				"decimals": 10,
				"status":   "enabled",
			},
		},
	}
	srv := fakeServer(t, resp)
	defer srv.Close()

	c := newTestClient(srv)
	assets, err := c.GetAssets(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 {
		t.Errorf("got %d assets, want 2", len(assets))
	}
	for _, a := range assets {
		if a.ID == "" {
			t.Error("asset ID should not be empty")
		}
		if a.AltName == "" {
			t.Error("asset AltName should not be empty")
		}
		if a.Status != "enabled" {
			t.Errorf("Status = %q, want enabled", a.Status)
		}
	}
}

func TestGetAssetsLimit(t *testing.T) {
	result := map[string]interface{}{}
	for i := 0; i < 5; i++ {
		key := string(rune('A'+i)) + "TOK"
		result[key] = map[string]interface{}{
			"altname":  key,
			"decimals": 8,
			"status":   "enabled",
		}
	}
	resp := map[string]interface{}{"error": []string{}, "result": result}
	srv := fakeServer(t, resp)
	defer srv.Close()

	c := newTestClient(srv)
	assets, err := c.GetAssets(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 3 {
		t.Errorf("got %d assets with limit 3, want 3", len(assets))
	}
}

// --- Pairs ---

func TestGetPairs(t *testing.T) {
	resp := map[string]interface{}{
		"error": []string{},
		"result": map[string]interface{}{
			"XXBTZUSD": map[string]interface{}{
				"altname": "XBTUSD",
				"base":    "XXBT",
				"quote":   "ZUSD",
				"status":  "online",
			},
		},
	}
	srv := fakeServer(t, resp)
	defer srv.Close()

	c := newTestClient(srv)
	pairs, err := c.GetPairs(context.Background(), "XBTUSD", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs, want 1", len(pairs))
	}
	p := pairs[0]
	if p.ID != "XXBTZUSD" {
		t.Errorf("ID = %q, want XXBTZUSD", p.ID)
	}
	if p.AltName != "XBTUSD" {
		t.Errorf("AltName = %q, want XBTUSD", p.AltName)
	}
	if p.Base != "XXBT" {
		t.Errorf("Base = %q, want XXBT", p.Base)
	}
	if p.Quote != "ZUSD" {
		t.Errorf("Quote = %q, want ZUSD", p.Quote)
	}
}

// --- OHLC ---

func TestGetOHLC(t *testing.T) {
	resp := map[string]interface{}{
		"error": []string{},
		"result": map[string]interface{}{
			"XXBTZUSD": []interface{}{
				[]interface{}{float64(1700000000), "64000.00", "65000.00", "63500.00", "64800.00", "64300.00", "123.45", float64(200)},
				[]interface{}{float64(1700086400), "64800.00", "66000.00", "64500.00", "65500.00", "65100.00", "98.76", float64(150)},
			},
			"last": float64(1700086400),
		},
	}
	srv := fakeServer(t, resp)
	defer srv.Close()

	c := newTestClient(srv)
	candles, err := c.GetOHLC(context.Background(), "XBTUSD", 1440, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 2 {
		t.Fatalf("got %d candles, want 2", len(candles))
	}
	first := candles[0]
	if first.Pair != "XXBTZUSD" {
		t.Errorf("Pair = %q, want XXBTZUSD", first.Pair)
	}
	if first.Time != 1700000000 {
		t.Errorf("Time = %d, want 1700000000", first.Time)
	}
	if first.Open != "64000.00" {
		t.Errorf("Open = %q, want 64000.00", first.Open)
	}
	if first.Close != "64800.00" {
		t.Errorf("Close = %q, want 64800.00", first.Close)
	}
	if first.Count != 200 {
		t.Errorf("Count = %d, want 200", first.Count)
	}
}

func TestGetOHLCLimit(t *testing.T) {
	rows := make([]interface{}, 5)
	for i := range rows {
		rows[i] = []interface{}{
			float64(1700000000 + i*86400),
			"64000.00", "65000.00", "63500.00", "64800.00",
			"64300.00", "100.00", float64(50),
		}
	}
	resp := map[string]interface{}{
		"error": []string{},
		"result": map[string]interface{}{
			"XXBTZUSD": rows,
			"last":     float64(1700000000),
		},
	}
	srv := fakeServer(t, resp)
	defer srv.Close()

	c := newTestClient(srv)
	candles, err := c.GetOHLC(context.Background(), "XBTUSD", 1440, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 3 {
		t.Errorf("got %d candles with limit 3, want 3", len(candles))
	}
}
