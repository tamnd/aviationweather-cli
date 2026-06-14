// Package aviationweather is the library behind the aviationweather command line:
// the HTTP client, request shaping, and the typed data models for the
// Aviation Weather Center API (aviationweather.gov/api/data).
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package aviationweather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to aviationweather.gov.
const DefaultUserAgent = "aviationweather-cli/dev (+https://github.com/tamnd/aviationweather-cli)"

// Host is the site this client talks to.
const Host = "aviationweather.gov"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// apiBase is the data API prefix.
const apiBase = BaseURL + "/api/data"

// Client talks to aviationweather.gov over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 30 s timeout, a 200 ms
// minimum gap between requests, and five retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
	}
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
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

// ---------- output types ----------

// METAR is a single aviation weather observation.
type METAR struct {
	Station    string  `kit:"id" json:"station"`
	ObsTime    string  `json:"obs_time"`
	Raw        string  `json:"raw"`
	Temp       float64 `json:"temp_c"`
	Dewpoint   float64 `json:"dewpoint_c"`
	WindDir    int     `json:"wind_dir"`
	WindSpeed  int     `json:"wind_speed"`
	WindGust   int     `json:"wind_gust"`
	Visibility float64 `json:"visibility_sm"`
	Altimeter  float64 `json:"altimeter_inhg"`
	WxString   string  `json:"wx_string"`
	SkyString  string  `json:"sky"` // joined from sky array, e.g. "FEW020 SCT050"
}

// TAF is a terminal aerodrome forecast.
type TAF struct {
	Station   string `kit:"id" json:"station"`
	IssueTime string `json:"issue_time"`
	Raw       string `json:"raw"`
}

// Airport holds static airport information.
type Airport struct {
	ICAO      string  `kit:"id" json:"icao"`
	IATA      string  `json:"iata"`
	Name      string  `json:"name"`
	State     string  `json:"state"`
	Country   string  `json:"country"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Elevation int     `json:"elevation_ft"`
}

// SIGMET is a significant meteorological information notice.
type SIGMET struct {
	Type     string `kit:"id" json:"type"`
	Hazard   string `json:"hazard"`
	Severity string `json:"severity"`
	AltLow   int    `json:"alt_low_ft"`
	AltHigh  int    `json:"alt_high_ft"`
}

// ---------- wire types ----------

type wireSkyLayer struct {
	Cover string `json:"cover"`
	Base  int    `json:"base"`
}

type wireMetar struct {
	StationId string         `json:"stationId"`
	ObsTime   string         `json:"obsTime"`
	RawOb     string         `json:"rawOb"`
	Temp      float64        `json:"temp"`
	Dewpoint  float64        `json:"dewpoint"`
	Wdir      int            `json:"wdir"`
	Wspd      int            `json:"wspd"`
	Wgst      int            `json:"wgst"`
	Visib     float64        `json:"visib"`
	Altim     float64        `json:"altim"`
	WxString  string         `json:"wxString"`
	Sky       []wireSkyLayer `json:"sky"`
}

type wireTaf struct {
	StationId string `json:"stationId"`
	IssueTime string `json:"issueTime"`
	RawTAF    string `json:"rawTAF"`
}

type wireAirport struct {
	IcaoId  string  `json:"icaoId"`
	IataId  string  `json:"iataId"`
	Site    string  `json:"site"`
	State   string  `json:"state"`
	Country string  `json:"country"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Elev    int     `json:"elev"`
}

type wireSigmet struct {
	AirsigmetType string `json:"airsigmetType"`
	Hazard        string `json:"hazard"`
	Severity      string `json:"severity"`
	AltitudeLow1  int    `json:"altitudeLow1"`
	AltitudeHi1   int    `json:"altitudeHi1"`
}

// ---------- API methods ----------

// GetMETAR fetches METAR observations for the given comma-separated station ids.
func (c *Client) GetMETAR(ctx context.Context, ids string) ([]*METAR, error) {
	return c.GetMETARFromURL(ctx, apiBase+"/metar?ids="+ids+"&format=json")
}

// GetMETARFromURL fetches METAR observations from an explicit URL (useful for testing).
func (c *Client) GetMETARFromURL(ctx context.Context, url string) ([]*METAR, error) {
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var raw []wireMetar
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode metar: %w", err)
	}
	out := make([]*METAR, 0, len(raw))
	for _, w := range raw {
		out = append(out, &METAR{
			Station:    w.StationId,
			ObsTime:    w.ObsTime,
			Raw:        w.RawOb,
			Temp:       w.Temp,
			Dewpoint:   w.Dewpoint,
			WindDir:    w.Wdir,
			WindSpeed:  w.Wspd,
			WindGust:   w.Wgst,
			Visibility: w.Visib,
			Altimeter:  w.Altim,
			WxString:   w.WxString,
			SkyString:  joinSky(w.Sky),
		})
	}
	return out, nil
}

// GetTAF fetches TAF forecasts for the given comma-separated station ids.
func (c *Client) GetTAF(ctx context.Context, ids string) ([]*TAF, error) {
	return c.GetTAFFromURL(ctx, apiBase+"/taf?ids="+ids+"&format=json")
}

// GetTAFFromURL fetches TAF forecasts from an explicit URL (useful for testing).
func (c *Client) GetTAFFromURL(ctx context.Context, url string) ([]*TAF, error) {
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var raw []wireTaf
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode taf: %w", err)
	}
	out := make([]*TAF, 0, len(raw))
	for _, w := range raw {
		out = append(out, &TAF{
			Station:   w.StationId,
			IssueTime: w.IssueTime,
			Raw:       w.RawTAF,
		})
	}
	return out, nil
}

// GetAirport fetches airport information for the given comma-separated ICAO ids.
func (c *Client) GetAirport(ctx context.Context, ids string) ([]*Airport, error) {
	return c.GetAirportFromURL(ctx, apiBase+"/airport?ids="+ids+"&format=json")
}

// GetAirportFromURL fetches airport information from an explicit URL (useful for testing).
func (c *Client) GetAirportFromURL(ctx context.Context, url string) ([]*Airport, error) {
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var raw []wireAirport
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode airport: %w", err)
	}
	out := make([]*Airport, 0, len(raw))
	for _, w := range raw {
		out = append(out, &Airport{
			ICAO:      w.IcaoId,
			IATA:      w.IataId,
			Name:      w.Site,
			State:     w.State,
			Country:   w.Country,
			Lat:       w.Lat,
			Lon:       w.Lon,
			Elevation: w.Elev,
		})
	}
	return out, nil
}

// GetSIGMET fetches all active SIGMETs.
func (c *Client) GetSIGMET(ctx context.Context) ([]*SIGMET, error) {
	return c.GetSIGMETFromURL(ctx, apiBase+"/sigmet?format=json")
}

// GetSIGMETFromURL fetches SIGMETs from an explicit URL (useful for testing).
func (c *Client) GetSIGMETFromURL(ctx context.Context, url string) ([]*SIGMET, error) {
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var raw []wireSigmet
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode sigmet: %w", err)
	}
	out := make([]*SIGMET, 0, len(raw))
	for _, w := range raw {
		out = append(out, &SIGMET{
			Type:     w.AirsigmetType,
			Hazard:   w.Hazard,
			Severity: w.Severity,
			AltLow:   w.AltitudeLow1,
			AltHigh:  w.AltitudeHi1,
		})
	}
	return out, nil
}

// joinSky formats a list of sky layers as "FEW020 SCT050 BKN200".
func joinSky(layers []wireSkyLayer) string {
	parts := make([]string, 0, len(layers))
	for _, l := range layers {
		if l.Cover == "" {
			continue
		}
		// base is in hundreds of feet
		parts = append(parts, fmt.Sprintf("%s%03d", l.Cover, l.Base/100))
	}
	return strings.Join(parts, " ")
}
