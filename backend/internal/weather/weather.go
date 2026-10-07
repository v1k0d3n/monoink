// Package weather fetches forecasts from Open-Meteo (no API key, no
// account). It is only used when the user has chosen a location.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	forecastURL = "https://api.open-meteo.com/v1/forecast"
	geocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
)

type Day struct {
	Date     time.Time
	Code     int
	Max, Min float64
	Precip   int // max precipitation probability, percent
}

type Report struct {
	Fetched   time.Time
	Temp      float64
	FeelsLike float64
	Humidity  int
	Wind      float64
	Code      int
	IsDay     bool
	Days      []Day
	Imperial  bool
}

type Place struct {
	Name      string  `json:"name"`
	Admin     string  `json:"admin1"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (p Place) Label() string {
	s := p.Name
	if p.Admin != "" && p.Admin != p.Name {
		s += ", " + p.Admin
	}
	if p.Country != "" {
		s += ", " + p.Country
	}
	return s
}

type Client struct {
	HTTP        *http.Client
	ForecastURL string
	GeocodeURL  string
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}, ForecastURL: forecastURL, GeocodeURL: geocodeURL}
}

func (c *Client) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "monoink (+https://github.com/v1k0d3n/monoink)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", req.URL.Host, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Search resolves a place name to coordinates.
func (c *Client) Search(ctx context.Context, name string) ([]Place, error) {
	q := url.Values{"name": {name}, "count": {"8"}, "format": {"json"}}
	var body struct {
		Results []Place `json:"results"`
	}
	if err := c.get(ctx, c.GeocodeURL+"?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	return body.Results, nil
}

// Forecast fetches current conditions and a 5-day outlook.
func (c *Client) Forecast(ctx context.Context, lat, lon float64, imperial bool) (*Report, error) {
	q := url.Values{
		"latitude":      {strconv.FormatFloat(lat, 'f', 4, 64)},
		"longitude":     {strconv.FormatFloat(lon, 'f', 4, 64)},
		"current":       {"temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,weather_code,is_day"},
		"daily":         {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"},
		"timezone":      {"auto"},
		"forecast_days": {"5"},
	}
	if imperial {
		q.Set("temperature_unit", "fahrenheit")
		q.Set("wind_speed_unit", "mph")
	}
	var body struct {
		Current struct {
			Temp      float64 `json:"temperature_2m"`
			FeelsLike float64 `json:"apparent_temperature"`
			Humidity  int     `json:"relative_humidity_2m"`
			Wind      float64 `json:"wind_speed_10m"`
			Code      int     `json:"weather_code"`
			IsDay     int     `json:"is_day"`
		} `json:"current"`
		Daily struct {
			Time   []string  `json:"time"`
			Code   []int     `json:"weather_code"`
			Max    []float64 `json:"temperature_2m_max"`
			Min    []float64 `json:"temperature_2m_min"`
			Precip []*int    `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := c.get(ctx, c.ForecastURL+"?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	r := &Report{
		Fetched: time.Now(), Temp: body.Current.Temp, FeelsLike: body.Current.FeelsLike,
		Humidity: body.Current.Humidity, Wind: body.Current.Wind, Code: body.Current.Code,
		IsDay: body.Current.IsDay == 1, Imperial: imperial,
	}
	d := body.Daily
	for i := range d.Time {
		if i >= len(d.Code) || i >= len(d.Max) || i >= len(d.Min) {
			break
		}
		day := Day{Code: d.Code[i], Max: d.Max[i], Min: d.Min[i]}
		day.Date, _ = time.Parse("2006-01-02", d.Time[i])
		if i < len(d.Precip) && d.Precip[i] != nil {
			day.Precip = *d.Precip[i]
		}
		r.Days = append(r.Days, day)
	}
	return r, nil
}

// Kind groups WMO weather codes into the icons we can draw.
type Kind int

const (
	Clear Kind = iota
	PartlyCloudy
	Cloudy
	Fog
	Drizzle
	Rain
	Snow
	Storm
)

func KindOf(code int) Kind {
	switch {
	case code == 0:
		return Clear
	case code <= 2:
		return PartlyCloudy
	case code == 3:
		return Cloudy
	case code == 45 || code == 48:
		return Fog
	case code >= 51 && code <= 57:
		return Drizzle
	case (code >= 61 && code <= 67) || (code >= 80 && code <= 82):
		return Rain
	case (code >= 71 && code <= 77) || code == 85 || code == 86:
		return Snow
	case code >= 95:
		return Storm
	}
	return Cloudy
}

// Describe returns a short English description of a WMO code.
func Describe(code int) string {
	switch code {
	case 0:
		return "Clear"
	case 1:
		return "Mostly clear"
	case 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45, 48:
		return "Fog"
	case 51, 53, 55:
		return "Drizzle"
	case 56, 57:
		return "Freezing drizzle"
	case 61, 63:
		return "Rain"
	case 65:
		return "Heavy rain"
	case 66, 67:
		return "Freezing rain"
	case 71, 73, 75, 77:
		return "Snow"
	case 80, 81, 82:
		return "Showers"
	case 85, 86:
		return "Snow showers"
	case 95:
		return "Thunderstorm"
	case 96, 99:
		return "Thunderstorm, hail"
	}
	return "—"
}
