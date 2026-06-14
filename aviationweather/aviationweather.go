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
const DefaultUserAgent = "aviationweather-cli/0.1 (tamnd87@gmail.com)"

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

// NewClient returns a Client with sensible defaults: a 15 s timeout, a 500 ms
// minimum gap between requests, and three retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      500 * time.Millisecond,
		Retries:   3,
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
	ID         int     `json:"id"         kit:"id"`
	Station    string  `json:"station"`
	Raw        string  `json:"raw"`
	Time       string  `json:"time"`
	Temp       float64 `json:"temp"`
	Dewpoint   float64 `json:"dewpoint"`
	WindDir    int     `json:"wind_dir"`
	WindSpeed  int     `json:"wind_speed"`
	Visibility string  `json:"visibility"`
	Altimeter  float64 `json:"altimeter"`
}

// TAF is a terminal aerodrome forecast.
type TAF struct {
	ID        int    `json:"id"         kit:"id"`
	Station   string `json:"station"`
	Raw       string `json:"raw"`
	Time      string `json:"time"`
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to"`
}

// ---------- wire types ----------

type wireMetar struct {
	MetarID    int             `json:"metar_id"`
	IcaoId     string          `json:"icaoId"`
	RawOb      string          `json:"rawOb"`
	ReportTime string          `json:"reportTime"`
	Temp       *float64        `json:"temp"`
	Dewp       *float64        `json:"dewp"`
	Wdir       int             `json:"wdir"`
	Wspd       int             `json:"wspd"`
	Visib      json.RawMessage `json:"visib"`
	Altim      *float64        `json:"altim"`
}

type wireTaf struct {
	TafID         int    `json:"taf_id"`
	IcaoId        string `json:"icaoId"`
	RawTAF        string `json:"rawTAF"`
	ReportTime    string `json:"reportTime"`
	ValidTimeFrom string `json:"validTimeFrom"`
	ValidTimeTo   string `json:"validTimeTo"`
}

// ---------- API methods ----------

// GetMETAR fetches METAR observations for the given comma-separated station ids.
// hours controls how far back to look (default 1).
func (c *Client) GetMETAR(ctx context.Context, ids string, hours int) ([]*METAR, error) {
	if hours <= 0 {
		hours = 1
	}
	url := fmt.Sprintf("%s/metar?ids=%s&format=json&hours=%d", apiBase, ids, hours)
	return c.GetMETARFromURL(ctx, url)
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
		var temp, dewp, altim float64
		if w.Temp != nil {
			temp = *w.Temp
		}
		if w.Dewp != nil {
			dewp = *w.Dewp
		}
		if w.Altim != nil {
			altim = *w.Altim
		}
		out = append(out, &METAR{
			ID:         w.MetarID,
			Station:    w.IcaoId,
			Raw:        w.RawOb,
			Time:       w.ReportTime,
			Temp:       temp,
			Dewpoint:   dewp,
			WindDir:    w.Wdir,
			WindSpeed:  w.Wspd,
			Visibility: parseVisib(w.Visib),
			Altimeter:  altim,
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
			ID:        w.TafID,
			Station:   w.IcaoId,
			Raw:       w.RawTAF,
			Time:      w.ReportTime,
			ValidFrom: w.ValidTimeFrom,
			ValidTo:   w.ValidTimeTo,
		})
	}
	return out, nil
}

// parseVisib extracts a visibility string from the raw JSON value, which may be
// a JSON number (6.21) or a quoted string ("10+"). Returns the value as-is for
// strings, or formatted as %.0f for numbers.
func parseVisib(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first (e.g. "10+")
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Fall back to number
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return fmt.Sprintf("%.0f", f)
	}
	return strings.TrimSpace(string(raw))
}
