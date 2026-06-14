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
			"metar_id":   12345,
			"icaoId":     "KJFK",
			"rawOb":      "METAR KJFK 141551Z 09007KT 10SM FEW250 23/12 A3006",
			"reportTime": "2026-06-14 15:51:00",
			"temp":       23.0,
			"dewp":       12.0,
			"wdir":       90,
			"wspd":       7,
			"visib":      "10",
			"altim":      29.91,
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
	if m.ID != 12345 {
		t.Errorf("ID = %d, want 12345", m.ID)
	}
	if m.Station != "KJFK" {
		t.Errorf("Station = %q, want KJFK", m.Station)
	}
	if m.WindDir != 90 {
		t.Errorf("WindDir = %d, want 90", m.WindDir)
	}
	if m.WindSpeed != 7 {
		t.Errorf("WindSpeed = %d, want 7", m.WindSpeed)
	}
	if m.Temp != 23.0 {
		t.Errorf("Temp = %v, want 23.0", m.Temp)
	}
	if m.Dewpoint != 12.0 {
		t.Errorf("Dewpoint = %v, want 12.0", m.Dewpoint)
	}
	if m.Altimeter != 29.91 {
		t.Errorf("Altimeter = %v, want 29.91", m.Altimeter)
	}
	if m.Visibility != "10" {
		t.Errorf("Visibility = %q, want 10", m.Visibility)
	}
	if m.Time != "2026-06-14 15:51:00" {
		t.Errorf("Time = %q, want 2026-06-14 15:51:00", m.Time)
	}
	if m.Raw == "" {
		t.Error("Raw is empty")
	}
}

func TestGetMETARNullFields(t *testing.T) {
	// temp/dewp/altim can be null for some stations
	payload := []map[string]any{
		{
			"metar_id":   99,
			"icaoId":     "KSFO",
			"rawOb":      "METAR KSFO 141800Z 00000KT 10SM CLR 18/10 A3010",
			"reportTime": "2026-06-14 18:00:00",
			"temp":       nil,
			"dewp":       nil,
			"wdir":       0,
			"wspd":       0,
			"visib":      "10+",
			"altim":      nil,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := aviationweather.NewClient()
	c.Rate = 0
	records, err := c.GetMETARFromURL(context.Background(), srv.URL+"/api/data/metar?ids=KSFO&format=json")
	if err != nil {
		t.Fatalf("null fields should not error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	m := records[0]
	if m.Temp != 0 {
		t.Errorf("Temp = %v, want 0 for null", m.Temp)
	}
	if m.Visibility != "10+" {
		t.Errorf("Visibility = %q, want 10+", m.Visibility)
	}
}

func TestGetMETAREmpty(t *testing.T) {
	// API returns empty array when no results -- must not error.
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
			"taf_id":        67890,
			"icaoId":        "KLAX",
			"rawTAF":        "TAF KLAX 142031Z 1421/1524 26015KT P6SM SCT009",
			"reportTime":    "2026-06-14 20:31:00",
			"validTimeFrom": "2026-06-14 21:00:00",
			"validTimeTo":   "2026-06-15 24:00:00",
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
	if taf.ID != 67890 {
		t.Errorf("ID = %d, want 67890", taf.ID)
	}
	if taf.Station != "KLAX" {
		t.Errorf("Station = %q, want KLAX", taf.Station)
	}
	if taf.Time != "2026-06-14 20:31:00" {
		t.Errorf("Time = %q", taf.Time)
	}
	if taf.ValidFrom != "2026-06-14 21:00:00" {
		t.Errorf("ValidFrom = %q", taf.ValidFrom)
	}
	if taf.ValidTo != "2026-06-15 24:00:00" {
		t.Errorf("ValidTo = %q", taf.ValidTo)
	}
	if taf.Raw == "" {
		t.Error("Raw is empty")
	}
}

func TestGetTAFMultiple(t *testing.T) {
	payload := []map[string]any{
		{
			"taf_id": 1, "icaoId": "KSFO",
			"rawTAF": "TAF KSFO 141720Z ...", "reportTime": "2026-06-14 17:20:00",
			"validTimeFrom": "2026-06-14 18:00:00", "validTimeTo": "2026-06-15 18:00:00",
		},
		{
			"taf_id": 2, "icaoId": "KLAX",
			"rawTAF": "TAF KLAX 141720Z ...", "reportTime": "2026-06-14 17:20:00",
			"validTimeFrom": "2026-06-14 18:00:00", "validTimeTo": "2026-06-15 18:00:00",
		},
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
