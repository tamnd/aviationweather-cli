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
	payload := []map[string]any{
		{
			"stationId": "KJFK",
			"obsTime":   "2024-01-14T15:51:00Z",
			"rawOb":     "KJFK 141551Z 09007KT 10SM FEW250 23/12 A3006",
			"temp":      23.0,
			"dewpoint":  12.0,
			"wdir":      90,
			"wspd":      7,
			"wgst":      0,
			"visib":     10.0,
			"altim":     30.06,
			"wxString":  "",
			"sky":       []map[string]any{{"cover": "FEW", "base": 25000}},
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
	// point client at test server by replacing Host in URL via Get directly
	records, err := c.GetMETARFromURL(context.Background(), srv.URL+"/api/data/metar?ids=KJFK&format=json")
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
	if m.WindDir != 90 {
		t.Errorf("WindDir = %d, want 90", m.WindDir)
	}
	if m.SkyString != "FEW250" {
		t.Errorf("SkyString = %q, want FEW250", m.SkyString)
	}
}

func TestGetTAF(t *testing.T) {
	payload := []map[string]any{
		{
			"stationId": "KLAX",
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
	if records[0].Station != "KLAX" {
		t.Errorf("Station = %q, want KLAX", records[0].Station)
	}
	if records[0].Raw == "" {
		t.Error("Raw is empty")
	}
}

func TestGetAirport(t *testing.T) {
	payload := []map[string]any{
		{
			"icaoId":  "KORD",
			"iataId":  "ORD",
			"site":    "Chicago O'Hare Intl",
			"state":   "IL",
			"country": "US",
			"lat":     41.98,
			"lon":     -87.90,
			"elev":    668,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetAirportFromURL(context.Background(), srv.URL+"/api/data/airport?ids=KORD&format=json")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	a := records[0]
	if a.ICAO != "KORD" {
		t.Errorf("ICAO = %q, want KORD", a.ICAO)
	}
	if a.IATA != "ORD" {
		t.Errorf("IATA = %q, want ORD", a.IATA)
	}
	if a.Elevation != 668 {
		t.Errorf("Elevation = %d, want 668", a.Elevation)
	}
}

func TestGetSIGMET(t *testing.T) {
	payload := []map[string]any{
		{
			"airsigmetType": "SIGMET",
			"hazard":        "TS",
			"severity":      "MODERATE",
			"altitudeLow1":  0,
			"altitudeHi1":   45000,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetSIGMETFromURL(context.Background(), srv.URL+"/api/data/sigmet?format=json")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	s := records[0]
	if s.Type != "SIGMET" {
		t.Errorf("Type = %q, want SIGMET", s.Type)
	}
	if s.AltHigh != 45000 {
		t.Errorf("AltHigh = %d, want 45000", s.AltHigh)
	}
}

func TestGetMultipleStations(t *testing.T) {
	payload := []map[string]any{
		{"stationId": "KJFK", "obsTime": "", "rawOb": "KJFK raw", "sky": []map[string]any{}},
		{"stationId": "KLAX", "obsTime": "", "rawOb": "KLAX raw", "sky": []map[string]any{}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query().Get("ids")
		if ids != "KJFK,KLAX" {
			t.Errorf("ids = %q, want KJFK,KLAX", ids)
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetMETARFromURL(context.Background(), srv.URL+"/api/data/metar?ids=KJFK,KLAX&format=json")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
}
