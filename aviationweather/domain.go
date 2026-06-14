package aviationweather

import (
	"context"
	"regexp"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes the Aviation Weather Center as a kit Domain: a driver that
// a multi-domain host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/aviationweather-cli/aviationweather"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// aviationweather:// URIs by routing to the operations Register installs. The
// same Domain also builds the standalone aviationweather binary (see cli.NewApp),
// so the binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the Aviation Weather Center driver. It carries no state; the
// per-run client is built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "aviationweather",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "aviationweather",
			Short:  "A command line for the Aviation Weather Center API.",
			Long: `A command line for the Aviation Weather Center API.

aviationweather reads public aviation weather data over plain HTTPS, shapes it
into clean records, and prints output that pipes into the rest of your tools.
No API key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/aviationweather-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "metar", Group: "read", Single: true,
		Summary: "Fetch METAR observations for one or more stations",
		URIType: "station", Resolver: true,
		Args: []kit.Arg{{Name: "stations", Help: "ICAO station codes e.g. KJFK KLAX", Variadic: true}},
	}, getMETAR)

	kit.Handle(app, kit.OpMeta{
		Name: "taf", Group: "read", Single: true,
		Summary: "Fetch TAF forecasts for one or more stations",
		URIType: "station",
		Args:    []kit.Arg{{Name: "stations", Help: "ICAO station codes e.g. KSFO KLAX", Variadic: true}},
	}, getTAF)
}

// newClient builds the client from the host-resolved config.
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

type metarInput struct {
	Stations []string `kit:"arg,variadic" help:"ICAO station codes e.g. KJFK KLAX"`
	Hours    int      `kit:"flag" help:"how many hours back" default:"1"`
	Client   *Client  `kit:"inject"`
}

type tafInput struct {
	Stations []string `kit:"arg,variadic" help:"ICAO station codes e.g. KSFO KLAX"`
	Client   *Client  `kit:"inject"`
}

// --- handlers ---

func getMETAR(ctx context.Context, in metarInput, emit func(*METAR) error) error {
	ids := strings.Join(in.Stations, ",")
	hours := in.Hours
	if hours <= 0 {
		hours = 1
	}
	records, err := in.Client.GetMETAR(ctx, ids, hours)
	if err != nil {
		return err
	}
	for _, r := range records {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func getTAF(ctx context.Context, in tafInput, emit func(*TAF) error) error {
	ids := strings.Join(in.Stations, ",")
	records, err := in.Client.GetTAF(ctx, ids)
	if err != nil {
		return err
	}
	for _, r := range records {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: the URI-native string functions, pure and network-free ---

// icaoRE matches a 4-letter uppercase ICAO airport code.
var icaoRE = regexp.MustCompile(`^[A-Z]{4}$`)

// Classify turns any accepted input -- a bare uppercase ICAO code -- into the
// canonical (type, id), so `ant resolve` and `ant url` touch no network.
// Only 4-letter uppercase ICAO codes are accepted; lowercase is rejected.
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	code := strings.Trim(input, "/")
	if icaoRE.MatchString(code) {
		return "station", code, nil
	}
	return "", "", errs.Usage("unrecognized aviationweather reference: %q", input)
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "station" {
		return "", errs.Usage("aviationweather has no resource type %q", uriType)
	}
	return "https://" + Host + "/metar/data?ids=" + id + "&format=decoded&hours=0", nil
}
