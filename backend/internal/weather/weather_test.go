package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForecastAndSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/forecast":
			if r.URL.Query().Get("temperature_unit") != "fahrenheit" {
				t.Error("imperial units not requested")
			}
			w.Write([]byte(`{"current":{"temperature_2m":71.5,"apparent_temperature":70,"relative_humidity_2m":40,
				"wind_speed_10m":5,"weather_code":2,"is_day":1},
				"daily":{"time":["2026-10-07","2026-10-08"],"weather_code":[2,61],
				"temperature_2m_max":[75,68],"temperature_2m_min":[55,50],"precipitation_probability_max":[10,null]}}`))
		case "/search":
			w.Write([]byte(`{"results":[{"name":"Springfield","admin1":"Illinois","country":"United States","latitude":39.8,"longitude":-89.6}]}`))
		}
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), ForecastURL: srv.URL + "/forecast", GeocodeURL: srv.URL + "/search"}

	r, err := c.Forecast(context.Background(), 1, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Temp != 71.5 || len(r.Days) != 2 || r.Days[1].Code != 61 || r.Days[1].Precip != 0 || !r.IsDay {
		t.Fatalf("bad report %+v", r)
	}
	places, err := c.Search(context.Background(), "Springfield")
	if err != nil || len(places) != 1 || places[0].Label() != "Springfield, Illinois, United States" {
		t.Fatalf("search: %v %+v", err, places)
	}
}

func TestKindOf(t *testing.T) {
	for code, want := range map[int]Kind{0: Clear, 2: PartlyCloudy, 3: Cloudy, 45: Fog, 53: Drizzle, 63: Rain, 81: Rain, 73: Snow, 95: Storm} {
		if got := KindOf(code); got != want {
			t.Errorf("KindOf(%d) = %v, want %v", code, got, want)
		}
	}
}
