// Package screens renders the faceplate layouts. Renderers are pure: they
// take a Data snapshot and draw; all I/O happens elsewhere.
package screens

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"github.com/v1k0d3n/monoink/backend/internal/proto"
	"github.com/v1k0d3n/monoink/backend/internal/steam"
	"github.com/v1k0d3n/monoink/backend/internal/sysinfo"
	"github.com/v1k0d3n/monoink/backend/internal/weather"
)

const (
	W      = proto.Width
	H      = proto.Height
	margin = 24
	header = 52 // height of the title bar
)

// Card is content pushed by an approved provider.
type Card struct {
	Provider string    `json:"provider"`
	Title    string    `json:"title"`
	Lines    []string  `json:"lines"`
	Progress *float64  `json:"progress,omitempty"` // 0..1
	Updated  time.Time `json:"updated"`
	Expires  time.Time `json:"expires"`
}

// Data is everything a renderer may use.
type Data struct {
	Now           time.Time
	Clock24h      bool
	WeekStartsSun bool
	YearProgress  bool // show day/week of the year on the Clock screen
	Battery       int  // display battery percent, -1 unknown

	Weather    *weather.Report
	Place      string
	WeatherMsg string // shown when there's no report

	Sys     sysinfo.Snapshot
	History []sysinfo.Snapshot

	Game    *steam.Game
	GameArt image.Image

	Photo     image.Image
	PhotoName string
	PhotoFill bool // crop photos to fill the screen instead of fitting them

	Card *Card
}

// Screen describes one layout.
type Screen struct {
	ID      string
	Title   string
	Cadence time.Duration // how often content meaningfully changes
	Render  func(*Data) *Canvas
}

// All lists every screen keyed by ID.
var All = map[string]Screen{
	"clock":       {"clock", "Clock", time.Minute, Clock},
	"calendar":    {"calendar", "Calendar", time.Hour, Calendar},
	"weather":     {"weather", "Weather", 30 * time.Minute, Weather},
	"performance": {"performance", "Performance", time.Minute, Performance},
	"game":        {"game", "Game", 5 * time.Minute, Game},
	"photo":       {"photo", "Photo frame", 0, Photo}, // advanced by the photo timer
	"card":        {"card", "Provider card", 0, CardScreen},
	"dashboard":   {"dashboard", "Dashboard", time.Minute, Dashboard},
}

// ---- shared pieces -------------------------------------------------------

func (d *Data) timeString(t time.Time) string {
	if d.Clock24h {
		return t.Format("15:04")
	}
	return t.Format("3:04")
}

func (d *Data) ampm() string {
	if d.Clock24h {
		return ""
	}
	return d.Now.Format("PM")
}

// titleBar draws the header. withTime must only be set on screens whose
// Cadence is at most a minute; otherwise the clock would sit there stale
// until the screen's next redraw.
func titleBar(c *Canvas, d *Data, title string, withTime bool) {
	c.Text(strings.ToUpper(title), margin, 18, Bold, 22, Left, black)
	right := W - margin
	if d.Battery >= 0 {
		right -= battery(c, right, 18, d.Battery) + 16
	}
	label := d.Now.Format("Mon, Jan 2")
	if withTime {
		label = d.Now.Format("Mon Jan 2") + "  " + d.timeString(d.Now)
		if a := d.ampm(); a != "" {
			label += " " + a
		}
	}
	c.Text(label, right, 18, Medium, 20, Right, black)
	c.Fill(image.Rect(margin, header-3, W-margin, header), black)
}

// battery draws a small battery gauge ending at x and returns its width.
func battery(c *Canvas, x, y, pct int) int {
	bw, bh := 34, 18
	r := image.Rect(x-bw-3, y, x-3, y+bh)
	c.Box(r, 2, black)
	c.Fill(image.Rect(x-3, y+5, x, y+bh-5), black)
	fill := (bw - 8) * pct / 100
	c.Fill(image.Rect(r.Min.X+4, r.Min.Y+4, r.Min.X+4+fill, r.Max.Y-4), black)
	label := fmt.Sprintf("%d%%", pct)
	w := c.Text(label, r.Min.X-6, y+1, Regular, 16, Right, black)
	return bw + 6 + w
}

func message(c *Canvas, top int, lines ...string) {
	y := top + (H-top)/2 - len(lines)*20
	for i, l := range lines {
		w := Regular
		size := 22
		if i == 0 {
			w, size = Bold, 28
		}
		c.Text(l, W/2, y, w, size, Center, black)
		y += size + 18
	}
}

func bar(c *Canvas, r image.Rectangle, pct float64) {
	c.Box(r, 2, black)
	if pct < 0 {
		return
	}
	inner := r.Inset(4)
	w := int(float64(inner.Dx()) * math.Min(pct, 100) / 100)
	c.Fill(image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+w, inner.Max.Y), black)
}

func pctLabel(v float64) string {
	if v < 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", v)
}

func tempLabel(v float64) string {
	if v < 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f°C", v)
}

func durationLabel(d time.Duration) string {
	h := int(d.Hours())
	if h >= 1 {
		return fmt.Sprintf("%dh %02dm", h, int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// ---- clock ---------------------------------------------------------------

func Clock(d *Data) *Canvas {
	c := NewCanvas()
	// With year progress shown, everything moves up to make room for it.
	top := 110
	if d.YearProgress {
		top = 62
	}
	t := d.timeString(d.Now)
	c.Text(t, W/2, top, Bold, 190, Center, black)
	if a := d.ampm(); a != "" {
		w := Measure(t, Bold, 190)
		c.Text(a, W/2+w/2+8, top+10, Bold, 30, Left, black)
	}
	c.Text(d.Now.Format("Monday"), W/2, top+220, Medium, 40, Center, black)
	c.Text(d.Now.Format("January 2, 2006"), W/2, top+274, Regular, 30, Center, black)
	if d.YearProgress {
		yp := YearProgressAt(d.Now)
		y := top + 322
		c.Text(yp.String(), W/2, y, Regular, 20, Center, black)
		// Narrow enough to clear the weather and battery footer on both sides.
		bar(c, image.Rect(W/2-120, y+28, W/2+120, y+40), yp.Fraction()*100)
	}

	foot := ""
	if d.Weather != nil {
		foot = fmt.Sprintf("%s  %s", formatTemp(d.Weather.Temp, d.Weather.Imperial), weather.Describe(d.Weather.Code))
	}
	if foot != "" {
		c.Text(foot, margin, H-40, Regular, 20, Left, black)
	}
	if d.Battery >= 0 {
		battery(c, W-margin, H-42, d.Battery)
	}
	return c
}

// ---- calendar ------------------------------------------------------------

func Calendar(d *Data) *Canvas {
	c := NewCanvas()
	titleBar(c, d, "Calendar", false)
	c.Text(d.Now.Format("January 2006"), W/2, header+18, Bold, 30, Center, black)

	names := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	first := time.Monday
	if d.WeekStartsSun {
		names = append([]string{"Sun"}, names[:6]...)
		first = time.Sunday
	}
	gridTop, rowH := header+70, 50
	colW := (W - 2*margin) / 7
	for i, n := range names {
		c.Text(n, margin+colW*i+colW/2, gridTop, Medium, 18, Center, black)
	}
	c.Fill(image.Rect(margin, gridTop+28, W-margin, gridTop+30), black)

	y, m, _ := d.Now.Date()
	start := time.Date(y, m, 1, 0, 0, 0, 0, d.Now.Location())
	offset := (int(start.Weekday()) - int(first) + 7) % 7
	days := start.AddDate(0, 1, -1).Day()
	for day := 1; day <= days; day++ {
		idx := offset + day - 1
		col, row := idx%7, idx/7
		cx := margin + colW*col + colW/2
		cy := gridTop + 44 + row*rowH
		label := fmt.Sprint(day)
		if day == d.Now.Day() {
			c.Fill(image.Rect(cx-28, cy-8, cx+28, cy+38), black)
			c.Text(label, cx, cy, Bold, 26, Center, white)
		} else {
			c.Text(label, cx, cy, Regular, 26, Center, black)
		}
	}
	return c
}

// ---- weather -------------------------------------------------------------

func formatTemp(v float64, imperial bool) string {
	u := "C"
	if imperial {
		u = "F"
	}
	return fmt.Sprintf("%.0f°%s", v, u)
}

func Weather(d *Data) *Canvas {
	c := NewCanvas()
	titleBar(c, d, "Weather", false)
	r := d.Weather
	if r == nil {
		msg := d.WeatherMsg
		if msg == "" {
			msg = "Choose a location in the plugin settings."
		}
		message(c, header, "No weather yet", msg)
		return c
	}
	icon(c, weather.KindOf(r.Code), r.IsDay, 130, 170, 90)
	c.Text(formatTemp(r.Temp, r.Imperial), 250, 90, Bold, 96, Left, black)
	c.Text(weather.Describe(r.Code), 254, 200, Medium, 30, Left, black)
	wind := "km/h"
	if r.Imperial {
		wind = "mph"
	}
	detail := fmt.Sprintf("Feels %s · %d%% humidity · %.0f %s", formatTemp(r.FeelsLike, r.Imperial), r.Humidity, r.Wind, wind)
	c.Text(Fit(detail, Regular, 20, W-254-margin), 254, 244, Regular, 20, Left, black)
	place := "Updated " + d.timeString(r.Fetched) + " " + r.Fetched.Format("PM")
	if d.Clock24h {
		place = "Updated " + d.timeString(r.Fetched)
	}
	if d.Place != "" {
		place = d.Place + " · " + place
	}
	c.Text(Fit(place, Regular, 18, W-254-margin), 254, 276, Regular, 18, Left, black)

	c.Fill(image.Rect(margin, 314, W-margin, 316), black)
	n := len(r.Days)
	if n == 0 {
		return c
	}
	colW := (W - 2*margin) / n
	for i, day := range r.Days {
		cx := margin + colW*i + colW/2
		label := day.Date.Format("Mon")
		if i == 0 {
			label = "Today"
		}
		c.Text(label, cx, 328, Medium, 18, Center, black)
		icon(c, weather.KindOf(day.Code), true, float64(cx), 384, 26)
		c.Text(fmt.Sprintf("%.0f° / %.0f°", day.Max, day.Min), cx, 424, Regular, 18, Center, black)
		if day.Precip > 0 {
			c.Text(fmt.Sprintf("%d%%", day.Precip), cx, 450, Regular, 15, Center, black)
		}
	}
	return c
}

// icon draws a weather glyph centred at (cx, cy) with radius s.
func icon(c *Canvas, k weather.Kind, day bool, cx, cy, s float64) {
	sun := func(x, y, r float64) {
		if !day {
			c.Disc(x, y, r, black)
			c.Disc(x+r*0.45, y-r*0.3, r*0.85, white)
			return
		}
		c.Disc(x, y, r*0.55, black)
		for i := 0; i < 8; i++ {
			a := float64(i) * math.Pi / 4
			c.Line(x+math.Cos(a)*r*0.75, y+math.Sin(a)*r*0.75, x+math.Cos(a)*r, y+math.Sin(a)*r, r*0.12, black)
		}
	}
	cloud := func(x, y, r float64, fill bool) {
		col := black
		parts := [][3]float64{{-0.45, 0.1, 0.38}, {0.05, -0.15, 0.5}, {0.5, 0.12, 0.35}}
		for _, p := range parts {
			c.Disc(x+p[0]*r, y+p[1]*r, p[2]*r+r*0.08, col)
		}
		c.Fill(image.Rect(int(x-0.82*r), int(y+0.1*r), int(x+0.82*r), int(y+0.47*r)), col)
		if !fill {
			for _, p := range parts {
				c.Disc(x+p[0]*r, y+p[1]*r, p[2]*r-r*0.02, white)
			}
			c.Fill(image.Rect(int(x-0.72*r), int(y+0.1*r), int(x+0.72*r), int(y+0.37*r)), white)
		}
	}
	switch k {
	case weather.Clear:
		sun(cx, cy, s)
	case weather.PartlyCloudy:
		sun(cx-s*0.3, cy-s*0.3, s*0.7)
		cloud(cx+s*0.15, cy+s*0.2, s*0.8, false)
	case weather.Cloudy:
		cloud(cx, cy, s, false)
	case weather.Fog:
		for i := -2; i <= 2; i++ {
			off := float64(i%2) * s * 0.2
			c.Line(cx-s+off, cy+float64(i)*s*0.3, cx+s*0.8+off, cy+float64(i)*s*0.3, s*0.12, black)
		}
	case weather.Drizzle, weather.Rain, weather.Storm, weather.Snow:
		cloud(cx, cy-s*0.3, s, k == weather.Storm)
		for i := -1; i <= 1; i++ {
			x := cx + float64(i)*s*0.4
			switch k {
			case weather.Snow:
				c.Disc(x, cy+s*0.55, s*0.1, black)
				c.Disc(x-s*0.15, cy+s*0.85, s*0.1, black)
			case weather.Storm:
				if i == 0 {
					c.Polygon([][2]float64{{x, cy + s*0.2}, {x - s*0.25, cy + s*0.65}, {x - s*0.02, cy + s*0.65},
						{x - s*0.15, cy + s*1.05}, {x + s*0.25, cy + s*0.5}, {x + s*0.02, cy + s*0.5}, {x + s*0.12, cy + s*0.2}}, black)
				}
			default:
				w := s * 0.1
				if k == weather.Drizzle {
					w = s * 0.06
				}
				c.Line(x, cy+s*0.4, x-s*0.15, cy+s*0.85, w, black)
			}
		}
	}
}

// ---- performance ---------------------------------------------------------

func Performance(d *Data) *Canvas {
	c := NewCanvas()
	titleBar(c, d, "Performance", true)
	s := d.Sys
	rows := []struct {
		label string
		pct   float64
		extra string
	}{
		{"CPU", s.CPU, tempLabel(s.CPUTemp)},
		{"GPU", s.GPU, tempLabel(s.GPUTemp)},
		{"RAM", s.Mem, fmt.Sprintf("%.1f / %.1f GB", float64(s.MemUsed)/1e9, float64(s.MemTotal)/1e9)},
	}
	y := header + 24
	for _, r := range rows {
		c.Text(r.label, margin, y+6, Bold, 26, Left, black)
		bar(c, image.Rect(margin+80, y, W-margin-200, y+36), r.pct)
		c.Text(pctLabel(r.pct), W-margin-188, y+6, Bold, 26, Left, black)
		c.Text(r.extra, W-margin, y+10, Regular, 18, Right, black)
		y += 62
	}

	// 30-minute history: RAM as a dotted area, CPU as a solid line, GPU as
	// a line with hollow markers, so all three read clearly in 1-bit.
	g := image.Rect(margin, y+16, W-margin, H-44)
	c.Box(g, 2, black)
	var cpu, gpu, ram []float64
	for _, h := range d.History {
		cpu, gpu, ram = append(cpu, h.CPU), append(gpu, h.GPU), append(ram, h.Mem)
	}
	// Lift 0% off the frame so idle (near-zero) lines stay visible.
	in := image.Rect(g.Min.X+8, g.Min.Y+8, g.Max.X-8, g.Max.Y-12)
	plotArea(c, in, ram)
	plotLine(c, in, cpu, false)
	plotLine(c, in, gpu, true)

	x := c.Text("Last 30 min", margin, H-34, Regular, 16, Left, black) + margin + 18
	for _, item := range []struct {
		label  string
		swatch func(x, y int)
	}{
		{"CPU", func(x, y int) { c.Line(float64(x), float64(y), float64(x+22), float64(y), 3, black) }},
		{"GPU", func(x, y int) {
			c.Line(float64(x), float64(y), float64(x+22), float64(y), 3, black)
			c.Disc(float64(x+11), float64(y), 4, black)
			c.Disc(float64(x+11), float64(y), 2, white)
		}},
		{"RAM", func(x, y int) {
			r := image.Rect(x, y-6, x+22, y+6)
			dots(c, r)
			c.Box(r, 1, black)
		}},
	} {
		item.swatch(x, H-26)
		x += 28 + c.Text(item.label, x+28, H-34, Regular, 16, Left, black) + 16
	}
	if up := s.Uptime; up > 0 {
		c.Text("Up "+durationLabel(up), W-margin, H-34, Regular, 16, Right, black)
	}
	return c
}

// historyPoint maps sample i of n (newest last, 30 per graph) to a point
// in r, right-aligned so a short history grows in from the right edge.
func historyPoint(r image.Rectangle, i, n int, v float64) (float64, float64) {
	step := float64(r.Dx()) / 29
	x := float64(r.Max.X) - float64(n-1-i)*step
	y := float64(r.Max.Y) - math.Min(math.Max(v, 0), 100)/100*float64(r.Dy())
	return x, y
}

// plotLine draws a history series; markers adds hollow circles at each
// sample. Negative values (unknown) break the line.
func plotLine(c *Canvas, r image.Rectangle, vals []float64, markers bool) {
	if len(vals) < 2 {
		return
	}
	prevX, prevY := -1.0, -1.0
	for i, v := range vals {
		if v < 0 {
			prevX = -1
			continue
		}
		x, y := historyPoint(r, i, len(vals), v)
		if prevX >= 0 {
			c.Line(prevX, prevY, x, y, 3, black)
		}
		if markers {
			c.Disc(x, y, 4, black)
			c.Disc(x, y, 2, white)
		}
		prevX, prevY = x, y
	}
}

// plotArea shades the region under a history series with sparse dots.
func plotArea(c *Canvas, r image.Rectangle, vals []float64) {
	for i := 1; i < len(vals); i++ {
		if vals[i-1] < 0 || vals[i] < 0 {
			continue
		}
		x0, y0 := historyPoint(r, i-1, len(vals), vals[i-1])
		x1, y1 := historyPoint(r, i, len(vals), vals[i])
		for x := int(math.Ceil(x0)); x <= int(x1) && x < r.Max.X; x++ {
			t := (float64(x) - x0) / (x1 - x0)
			top := int(math.Ceil(y0 + t*(y1-y0)))
			dots(c, image.Rect(x, top, x+1, r.Max.Y))
		}
	}
}

// dots fills r with a sparse, regular dot pattern (one pixel in eight),
// light enough for lines drawn over it to stay readable.
func dots(c *Canvas, r image.Rectangle) {
	r = r.Intersect(c.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if x%4 == 0 && y%2 == 0 && (x/4+y/2)%2 == 0 {
				c.SetGray(x, y, black)
			}
		}
	}
}

// ---- game ----------------------------------------------------------------

func Game(d *Data) *Canvas {
	c := NewCanvas()
	titleBar(c, d, "Game", false)
	g := d.Game
	if g == nil {
		message(c, header, "No recent games", "Play something on Steam and it will show up here.")
		return c
	}
	art := image.Rect(margin, header+18, margin+272, H-20)
	textX := margin
	if d.GameArt != nil {
		c.Picture(d.GameArt, art, true)
		c.Box(art, 2, black)
		textX = art.Max.X + 28
	}
	maxW := W - margin - textX
	status := "LAST PLAYED"
	if g.Running {
		status = "NOW PLAYING"
	}
	y := header + 30
	c.Fill(image.Rect(textX, y-6, textX+Measure(status, Bold, 18)+20, y+26), black)
	c.Text(status, textX+10, y, Bold, 18, Left, white)
	y += 52
	for _, line := range Wrap(g.Name, Bold, 36, maxW, 3) {
		c.Text(line, textX, y, Bold, 36, Left, black)
		y += 48
	}
	y += 20
	stat := func(label, value string) {
		c.Text(label, textX, y, Regular, 18, Left, black)
		c.Text(value, textX, y+26, Bold, 30, Left, black)
		y += 76
	}
	if g.Playtime > 0 {
		stat("Total playtime", durationLabel(g.Playtime))
	}
	if !g.Running && !g.LastPlayed.IsZero() {
		stat("Last played", g.LastPlayed.Format("Mon, Jan 2"))
	}
	return c
}

// ---- photo ---------------------------------------------------------------

func Photo(d *Data) *Canvas {
	c := NewCanvas()
	if d.Photo == nil {
		titleBar(c, d, "Photo frame", false)
		message(c, header, "No photos", "Choose a folder with PNG, JPEG, GIF or WebP images.")
		return c
	}
	c.Picture(d.Photo, c.Bounds(), d.PhotoFill)
	return c
}

// ---- provider card -------------------------------------------------------

func CardScreen(d *Data) *Canvas {
	c := NewCanvas()
	card := d.Card
	if card == nil {
		titleBar(c, d, "Cards", false)
		message(c, header, "No cards", "Approved providers can push status cards here.")
		return c
	}
	titleBar(c, d, card.Provider, false)
	y := header + 26
	for _, line := range Wrap(card.Title, Bold, 40, W-2*margin, 2) {
		c.Text(line, margin, y, Bold, 40, Left, black)
		y += 52
	}
	y += 10
	for _, l := range card.Lines {
		if y > H-90 {
			break
		}
		c.Text(Fit(l, Regular, 26, W-2*margin), margin, y, Regular, 26, Left, black)
		y += 40
	}
	if card.Progress != nil {
		bar(c, image.Rect(margin, H-76, W-margin, H-40), *card.Progress*100)
	}
	c.Text("Updated "+card.Updated.Format("15:04"), W-margin, H-28, Regular, 14, Right, black)
	return c
}

// ---- dashboard -----------------------------------------------------------

func Dashboard(d *Data) *Canvas {
	c := NewCanvas()
	mid := W / 2
	// Top-left: time and date.
	t := d.timeString(d.Now)
	c.Text(t, margin, 30, Bold, 96, Left, black)
	if a := d.ampm(); a != "" {
		c.Text(a, margin+Measure(t, Bold, 96)+8, 36, Bold, 22, Left, black)
	}
	c.Text(d.Now.Format("Monday, January 2"), margin, 150, Medium, 24, Left, black)

	// Top-right: weather.
	if r := d.Weather; r != nil {
		icon(c, weather.KindOf(r.Code), r.IsDay, float64(mid+70), 90, 48)
		c.Text(formatTemp(r.Temp, r.Imperial), mid+140, 52, Bold, 52, Left, black)
		c.Text(Fit(weather.Describe(r.Code), Regular, 20, W-margin-mid-140), mid+140, 120, Regular, 20, Left, black)
		if len(r.Days) > 0 {
			c.Text(fmt.Sprintf("H %.0f°  L %.0f°", r.Days[0].Max, r.Days[0].Min), mid+140, 150, Regular, 20, Left, black)
		}
	} else if d.Battery >= 0 {
		c.Text("Display", W-margin, 60, Regular, 20, Right, black)
		battery(c, W-margin, 92, d.Battery)
	}

	c.Fill(image.Rect(margin, 196, W-margin, 199), black)
	c.Fill(image.Rect(mid-1, 214, mid+1, H-20), black)

	// Bottom-left: game.
	y := 216
	if g := d.Game; g != nil {
		x := margin
		if d.GameArt != nil {
			art := image.Rect(margin, y, margin+150, H-20)
			c.Picture(d.GameArt, art, true)
			c.Box(art, 2, black)
			x = art.Max.X + 14
		}
		maxW := mid - 16 - x
		label := "LAST PLAYED"
		if g.Running {
			label = "NOW PLAYING"
		}
		c.Text(label, x, y+4, Bold, 14, Left, black)
		yy := y + 30
		for _, l := range Wrap(g.Name, Bold, 22, maxW, 4) {
			c.Text(l, x, yy, Bold, 22, Left, black)
			yy += 30
		}
		if g.Playtime > 0 {
			c.Text(durationLabel(g.Playtime)+" total", x, H-44, Regular, 18, Left, black)
		}
	} else {
		c.Text("No recent games", margin, y+20, Regular, 20, Left, black)
	}

	// Bottom-right: system.
	s := d.Sys
	x := mid + 20
	rows := []struct {
		l string
		v float64
	}{{"CPU", s.CPU}, {"GPU", s.GPU}, {"RAM", s.Mem}}
	for i, r := range rows {
		ry := y + 6 + i*56
		c.Text(r.l, x, ry+4, Bold, 20, Left, black)
		bar(c, image.Rect(x+56, ry, W-margin-70, ry+28), r.v)
		c.Text(pctLabel(r.v), W-margin, ry+4, Bold, 20, Right, black)
	}
	foot := []string{}
	if s.CPUTemp >= 0 {
		foot = append(foot, "CPU "+tempLabel(s.CPUTemp))
	}
	if s.GPUTemp >= 0 {
		foot = append(foot, "GPU "+tempLabel(s.GPUTemp))
	}
	c.Text(strings.Join(foot, "  ·  "), x, H-44, Regular, 18, Left, black)
	return c
}
