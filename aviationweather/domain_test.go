package aviationweather

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The client's
// HTTP behaviour is covered in aviationweather_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "aviationweather" {
		t.Errorf("Scheme = %q, want aviationweather", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "aviationweather" {
		t.Errorf("Identity.Binary = %q, want aviationweather", info.Identity.Binary)
	}
}

func TestClassifyICAO(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"KJFK", "station", "KJFK"},
		{"KLAX", "station", "KLAX"},
		{"KORD", "station", "KORD"},
		{"EGLL", "station", "EGLL"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyInvalid(t *testing.T) {
	bad := []string{"", "JFK", "KJFKX", "kjfk", "1234"}
	for _, in := range bad {
		typ, id, err := Domain{}.Classify(in)
		if err == nil {
			t.Errorf("Classify(%q) = (%q, %q, nil), want error", in, typ, id)
		}
	}
}

func TestLocateStation(t *testing.T) {
	got, err := Domain{}.Locate("station", "KJFK")
	want := "https://aviationweather.gov/metar/data?ids=KJFK&format=decoded&hours=0"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("page", "anything")
	if err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	m := &METAR{Station: "KJFK", Raw: "KJFK 141551Z 09007KT 10SM FEW250 23/12 A3006"}
	u, err := h.Mint(m)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "aviationweather://station/KJFK"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("aviationweather", "KLAX")
	if err != nil || got.String() != "aviationweather://station/KLAX" {
		t.Errorf("ResolveOn = (%q, %v), want aviationweather://station/KLAX", got.String(), err)
	}
}

func TestJoinSky(t *testing.T) {
	layers := []wireSkyLayer{
		{Cover: "FEW", Base: 2000},
		{Cover: "SCT", Base: 5000},
		{Cover: "BKN", Base: 20000},
	}
	got := joinSky(layers)
	want := "FEW020 SCT050 BKN200"
	if got != want {
		t.Errorf("joinSky = %q, want %q", got, want)
	}
}

func TestJoinSkyEmpty(t *testing.T) {
	got := joinSky(nil)
	if got != "" {
		t.Errorf("joinSky(nil) = %q, want empty", got)
	}
}
