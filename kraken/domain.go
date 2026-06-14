package kraken

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes Kraken market data as a kit Domain. A multi-domain
// host (like ant) can blank-import this package to enable the kraken:// URI
// scheme:
//
//	import _ "github.com/tamnd/kraken-cli/kraken"
//
// The same Domain also builds the standalone kraken binary.
func init() { kit.Register(Domain{}) }

// Domain is the Kraken driver.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity used for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "kraken",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "kraken",
			Short:  "A command line for the Kraken crypto exchange.",
			Long: `A command line for the Kraken crypto exchange.

kraken reads public Kraken market data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools.
No API key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/kraken-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "ticker", Group: "market", Single: true,
		Summary: "Current price ticker for an asset pair (e.g. XBTUSD)",
		URIType: "pair", Resolver: true,
		Args: []kit.Arg{{Name: "pair", Help: "asset pair e.g. XBTUSD"}},
	}, getTicker)

	kit.Handle(app, kit.OpMeta{
		Name: "assets", Group: "market", List: true,
		Summary: "List all Kraken assets (currencies)",
		URIType: "asset",
	}, getAssets)

	kit.Handle(app, kit.OpMeta{
		Name: "pairs", Group: "market", List: true,
		Summary: "List Kraken tradeable asset pairs",
		URIType: "pair",
	}, getPairs)

	kit.Handle(app, kit.OpMeta{
		Name: "ohlc", Group: "market", List: true,
		Summary: "OHLC candlestick data for an asset pair",
		URIType: "pair",
		Args:    []kit.Arg{{Name: "pair", Help: "asset pair e.g. XBTUSD"}},
	}, getOHLC)
}

// newClient builds the Kraken client from the kit config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type tickerInput struct {
	Pair   string  `kit:"arg" help:"asset pair e.g. XBTUSD"`
	Client *Client `kit:"inject"`
}

type assetsInput struct {
	Limit  int     `kit:"flag,inherit"`
	Client *Client `kit:"inject"`
}

type pairsInput struct {
	Pairs  string  `kit:"flag" help:"comma-separated pairs, e.g. XBTUSD,ETHUSD"`
	Limit  int     `kit:"flag,inherit"`
	Client *Client `kit:"inject"`
}

type ohlcInput struct {
	Pair     string  `kit:"arg" help:"asset pair e.g. XBTUSD"`
	Interval int     `kit:"flag" help:"minutes: 1|5|15|60|1440" default:"1440"`
	Limit    int     `kit:"flag,inherit" help:"max candles" default:"10"`
	Client   *Client `kit:"inject"`
}

// --- handlers ---

func getTicker(ctx context.Context, in tickerInput, emit func(*Ticker) error) error {
	t, err := in.Client.GetTicker(ctx, in.Pair)
	if err != nil {
		return mapErr(err)
	}
	return emit(t)
}

func getAssets(ctx context.Context, in assetsInput, emit func(*Asset) error) error {
	assets, err := in.Client.GetAssets(ctx, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, a := range assets {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

func getPairs(ctx context.Context, in pairsInput, emit func(*Pair) error) error {
	pairs, err := in.Client.GetPairs(ctx, in.Pairs, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, p := range pairs {
		if err := emit(p); err != nil {
			return err
		}
	}
	return nil
}

func getOHLC(ctx context.Context, in ohlcInput, emit func(*Candle) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	candles, err := in.Client.GetOHLC(ctx, in.Pair, in.Interval, limit)
	if err != nil {
		return mapErr(err)
	}
	for _, c := range candles {
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver ---

// Classify turns a reference (pair name or URL) into the canonical (type, id).
// Pair-like inputs (contain "USD", "EUR", "BTC", "XBT", "ETH", or "USDT")
// classify as "pair"; other inputs classify as "asset".
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty kraken reference")
	}
	upper := strings.ToUpper(input)
	pairKeywords := []string{"USD", "EUR", "GBP", "BTC", "XBT", "ETH", "USDT", "USDC"}
	for _, kw := range pairKeywords {
		if strings.Contains(upper, kw) {
			return "pair", strings.ToUpper(strings.TrimSpace(input)), nil
		}
	}
	return "asset", strings.ToUpper(strings.TrimSpace(input)), nil
}

// Locate returns the live web URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "pair":
		return "https://www.kraken.com/charts/" + id, nil
	case "asset":
		return "https://www.kraken.com/markets", nil
	}
	return "", errs.Usage("kraken has no resource type %q", uriType)
}

// mapErr converts library errors to kit error kinds.
func mapErr(err error) error {
	return err
}
