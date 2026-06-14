package aviationweather_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/aviationweather-cli/aviationweather"
)

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
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

	c := aviationweather.NewClient()
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

func TestGetMETAR(t *testing.T) {
	// altim in the API is hPa; 1018.8 hPa ≈ 30.09 inHg
	payload := []map[string]any{
		{
			"icaoId":     "KJFK",
			"reportTime": "2024-01-14T15:51:00.000Z",
			"rawOb":      "METAR KJFK 141551Z 09007KT 10SM FEW250 23/12 A3006",
			"temp":       23.0,
			"dewp":       12.0,
			"wdir":       90,
			"wspd":       7,
			"visib":      "10",
			"altim":      1018.8,
			"wxString":   "",
			"clouds":     []map[string]any{{"cover": "FEW", "base": 25000}},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/data/metar" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetMETARFromURL(context.Background(), srv.URL+"/api/data/metar?ids=KJFK&format=json&hours=1")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	m := records[0]
	if m.Station != "KJFK" {
		t.Errorf("Station = %q, want KJFK", m.Station)
	}
	if m.Wind != "90°@7kt" {
		t.Errorf("Wind = %q, want 90°@7kt", m.Wind)
	}
	if m.Temp != "23.0" {
		t.Errorf("Temp = %q, want 23.0", m.Temp)
	}
	// 1018.8 hPa * 0.02953 ≈ 30.09 inHg
	if m.Altimeter != "30.09" {
		t.Errorf("Altimeter = %q, want 30.09", m.Altimeter)
	}
	if m.Visibility != "10" {
		t.Errorf("Visibility = %q, want 10", m.Visibility)
	}
}

func TestGetMETAREmpty(t *testing.T) {
	// API returns empty array when blocked from datacenter IPs — must not error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetMETARFromURL(context.Background(), srv.URL+"/api/data/metar?ids=KSFO&format=json")
	if err != nil {
		t.Fatalf("empty array should not error, got: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("got %d records, want 0", len(records))
	}
}

func TestGetTAF(t *testing.T) {
	payload := []map[string]any{
		{
			"icaoId":    "KLAX",
			"issueTime": "2024-01-14T12:00:00Z",
			"rawTAF":    "KLAX 141200Z 1412/1518 25010KT P6SM SKC",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetTAFFromURL(context.Background(), srv.URL+"/api/data/taf?ids=KLAX&format=json")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	taf := records[0]
	if taf.Station != "KLAX" {
		t.Errorf("Station = %q, want KLAX", taf.Station)
	}
	if taf.Issued != "2024-01-14T12:00:00Z" {
		t.Errorf("Issued = %q", taf.Issued)
	}
	if taf.Raw == "" {
		t.Error("Raw is empty")
	}
}

func TestGetTAFMultiple(t *testing.T) {
	payload := []map[string]any{
		{"icaoId": "KSFO", "issueTime": "2024-01-14T18:00:00Z", "rawTAF": "TAF KSFO 141720Z ..."},
		{"icaoId": "KLAX", "issueTime": "2024-01-14T18:00:00Z", "rawTAF": "TAF KLAX 141720Z ..."},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query().Get("ids")
		if ids != "KSFO,KLAX" {
			t.Errorf("ids = %q, want KSFO,KLAX", ids)
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetTAFFromURL(context.Background(), srv.URL+"/api/data/taf?ids=KSFO,KLAX&format=json")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
}

func TestGetStation(t *testing.T) {
	payload := []map[string]any{
		{
			"icaoId":  "KORD",
			"iataId":  "ORD",
			"name":    "Chicago O'Hare Intl Airport",
			"state":   "IL",
			"country": "US",
			"lat":     41.978,
			"lon":     -87.904,
			"elev":    668,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetStationFromURL(context.Background(), srv.URL+"/api/data/airport?ids=KORD&format=json")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	s := records[0]
	if s.ICAO != "KORD" {
		t.Errorf("ICAO = %q, want KORD", s.ICAO)
	}
	if s.IATA != "ORD" {
		t.Errorf("IATA = %q, want ORD", s.IATA)
	}
	if s.State != "IL" {
		t.Errorf("State = %q, want IL", s.State)
	}
	if s.Elev != 668 {
		t.Errorf("Elev = %d, want 668", s.Elev)
	}
	if s.Lat != "41.978" {
		t.Errorf("Lat = %q, want 41.978", s.Lat)
	}
}
