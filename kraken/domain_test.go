package kraken

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and host wiring, which need no network. HTTP behaviour is in kraken_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "kraken" {
		t.Errorf("Scheme = %q, want kraken", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "kraken" {
		t.Errorf("Identity.Binary = %q, want kraken", info.Identity.Binary)
	}
}

func TestClassifyPair(t *testing.T) {
	cases := []struct {
		in      string
		wantTyp string
		wantID  string
	}{
		{"XBTUSD", "pair", "XBTUSD"},
		{"xbtusd", "pair", "XBTUSD"},
		{"ETHUSD", "pair", "ETHUSD"},
		{"BTCUSD", "pair", "BTCUSD"},
		{"XXBTZUSD", "pair", "XXBTZUSD"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil {
			t.Errorf("Classify(%q): unexpected error %v", tc.in, err)
			continue
		}
		if typ != tc.wantTyp || id != tc.wantID {
			t.Errorf("Classify(%q) = (%q,%q), want (%q,%q)",
				tc.in, typ, id, tc.wantTyp, tc.wantID)
		}
	}
}

func TestClassifyAsset(t *testing.T) {
	// Assets that do NOT contain pair keywords classify as "asset".
	// Note: "XXBT" contains "XBT" so it classifies as "pair".
	// A pure asset symbol like "ALGO" (no USD/XBT/ETH/etc) is "asset".
	typ, id, err := Domain{}.Classify("ALGO")
	if err != nil {
		t.Fatalf("Classify(ALGO): %v", err)
	}
	if typ != "asset" || id != "ALGO" {
		t.Errorf("Classify(ALGO) = (%q,%q), want (asset,ALGO)", typ, id)
	}

	// Symbols containing pair keywords are classified as pairs.
	typ2, id2, err2 := Domain{}.Classify("XXBT")
	if err2 != nil {
		t.Fatalf("Classify(XXBT): %v", err2)
	}
	if typ2 != "pair" || id2 != "XXBT" {
		t.Errorf("Classify(XXBT) = (%q,%q), want (pair,XXBT)", typ2, id2)
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return error")
	}
}

func TestLocatePair(t *testing.T) {
	got, err := Domain{}.Locate("pair", "XBTUSD")
	if err != nil {
		t.Fatalf("Locate pair: %v", err)
	}
	want := "https://www.kraken.com/charts/XBTUSD"
	if got != want {
		t.Errorf("Locate pair = %q, want %q", got, want)
	}
}

func TestLocateAsset(t *testing.T) {
	got, err := Domain{}.Locate("asset", "XXBT")
	if err != nil {
		t.Fatalf("Locate asset: %v", err)
	}
	want := "https://www.kraken.com/markets"
	if got != want {
		t.Errorf("Locate asset = %q, want %q", got, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "foo")
	if err == nil {
		t.Error("Locate with unknown type should return error")
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	tk := &Ticker{Pair: "XXBTZUSD", Price: "65000.00"}
	u, err := h.Mint(tk)
	if err != nil {
		t.Fatalf("Mint Ticker: %v", err)
	}
	if want := "kraken://pair/XXBTZUSD"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("kraken", "XBTUSD")
	if err != nil {
		t.Fatalf("ResolveOn: %v", err)
	}
	if got.String() != "kraken://pair/XBTUSD" {
		t.Errorf("ResolveOn = %q, want kraken://pair/XBTUSD", got.String())
	}
}

func TestDecodeError(t *testing.T) {
	body := []byte(`{"error":["EQuery:Unknown asset pair"],"result":{}}`)
	_, err := decode(body)
	if err == nil {
		t.Fatal("expected error from API error response")
	}
	if err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

func TestDecodeOK(t *testing.T) {
	body := []byte(`{"error":[],"result":{"key":"value"}}`)
	result, err := decode(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(result) != `{"key":"value"}` {
		t.Errorf("result = %q, want {\"key\":\"value\"}", string(result))
	}
}
