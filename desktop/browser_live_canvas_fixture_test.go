//go:build windows && production && browserlive

package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/browserhost"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type browserLiveSize struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type browserLiveRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type browserLiveCanvasView struct {
	Observation int             `json:"observation"`
	ViewportCSS browserLiveSize `json:"viewport_css"`
	Image       browserLiveSize `json:"image_pixels"`
	CanvasCSS   browserLiveRect `json:"canvas_css"`
	TargetCSS   browserLiveRect `json:"target_css"`
}

type browserLiveCanvasDecision struct {
	Observation int      `json:"observation"`
	HasRef      bool     `json:"has_ref"`
	X           *float64 `json:"requested_image_x,omitempty"`
	Y           *float64 `json:"requested_image_y,omitempty"`
}

type browserLiveCanvasAction struct {
	Observation int     `json:"observation"`
	HasRef      bool    `json:"has_ref"`
	X           float64 `json:"action_css_x"`
	Y           float64 `json:"action_css_y"`
	Completed   bool    `json:"completed"`
	ErrorKind   string  `json:"errorKind,omitempty"`
}

type browserLiveCanvasPageClick struct {
	Category   string          `json:"category"`
	Trusted    bool            `json:"trusted"`
	X          float64         `json:"canvas_pixel_x"`
	Y          float64         `json:"canvas_pixel_y"`
	ClientX    float64         `json:"client_css_x"`
	ClientY    float64         `json:"client_css_y"`
	CanvasRect browserLiveRect `json:"canvas_css"`
	Viewport   browserLiveSize `json:"viewport_css"`
	DPR        float64         `json:"device_pixel_ratio"`
}

type browserLiveCanvasReport struct {
	CanvasPixels browserLiveSize              `json:"canvas_pixels"`
	TargetPixels browserLiveRect              `json:"target_pixels"`
	Views        []browserLiveCanvasView      `json:"observations,omitempty"`
	Decisions    []browserLiveCanvasDecision  `json:"model_clicks,omitempty"`
	Actions      []browserLiveCanvasAction    `json:"native_clicks,omitempty"`
	PageClicks   []browserLiveCanvasPageClick `json:"page_clicks,omitempty"`
	Invalid      map[string]int               `json:"invalid_categories,omitempty"`
}

type browserLiveCanvasTrace struct {
	page       *browserLivePage
	report     browserLiveCanvasReport
	observed   bool
	generation uint64
	document   string
}

func newBrowserLiveCanvasTrace(page *browserLivePage) *browserLiveCanvasTrace {
	if page == nil || page.name != "canvas" {
		return nil
	}
	return &browserLiveCanvasTrace{page: page, report: browserLiveCanvasReport{
		CanvasPixels: browserLiveSize{900, 600},
		TargetPixels: browserLiveRect{float64(page.canvasX), float64(page.canvasY), 100, 66},
	}}
}

func (t *browserLiveTrace) recordCanvasObservation(o browserhost.Observation) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.canvas
	if c == nil {
		return
	}
	c.observed = o.URL == c.page.address()
	if !c.observed {
		return
	}
	c.generation, c.document = o.Generation, o.DocumentID
	view := browserLiveCanvasView{Observation: len(c.report.Views) + 1,
		ViewportCSS: browserLiveSize{o.ViewportWidth, o.ViewportHeight},
		Image:       browserLiveSize{float64(o.ScreenshotWidth), float64(o.ScreenshotHeight)}}
	for _, e := range o.Elements {
		if strings.EqualFold(e.Role, "canvas") {
			view.CanvasCSS = browserLiveRect{e.X, e.Y, e.W, e.H}
			p := c.report.TargetPixels
			view.TargetCSS = browserLiveRect{e.X + p.X*e.W/900, e.Y + p.Y*e.H/600, p.Width * e.W / 900, p.Height * e.H / 600}
			break
		}
	}
	if len(c.report.Views) < 64 {
		c.report.Views = append(c.report.Views, view)
	}
}

func (t *browserLiveTrace) recordCanvasDecision(call provider.ToolCall) {
	if t == nil || call.Name != "browser_action" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.canvas
	if c == nil || !c.observed || len(c.report.Decisions) >= 64 {
		return
	}
	// Extract numeric diagnostics only; never retain arguments, tool IDs or text.
	var v struct {
		Kind      string   `json:"kind"`
		ElementID string   `json:"elementId"`
		X         *float64 `json:"x"`
		Y         *float64 `json:"y"`
	}
	if json.Unmarshal([]byte(call.Arguments), &v) != nil || v.Kind != "click" {
		return
	}
	c.report.Decisions = append(c.report.Decisions, browserLiveCanvasDecision{len(c.report.Views), v.ElementID != "", v.X, v.Y})
}

func (t *browserLiveTrace) recordCanvasAction(a browserhost.Action, err error) {
	if t == nil || a.Kind != "click" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.canvas
	if c == nil || !c.observed || c.generation != a.Generation || c.document != a.DocumentID || len(c.report.Actions) >= 64 {
		return
	}
	r := browserLiveCanvasAction{Observation: len(c.report.Views), HasRef: a.ElementID != "", X: a.X, Y: a.Y, Completed: err == nil}
	if err != nil {
		r.ErrorKind = browserLiveFailureCode(err)
	}
	c.report.Actions = append(c.report.Actions, r)
}

func (c *browserLiveCanvasTrace) snapshot() *browserLiveCanvasReport {
	r := c.report
	r.Views = append([]browserLiveCanvasView(nil), r.Views...)
	r.Decisions = append([]browserLiveCanvasDecision(nil), r.Decisions...)
	r.Actions = append([]browserLiveCanvasAction(nil), r.Actions...)
	c.page.mu.Lock()
	defer c.page.mu.Unlock()
	r.PageClicks = append([]browserLiveCanvasPageClick(nil), c.page.canvasEvents...)
	r.Invalid = make(map[string]int, len(c.page.canvasInvalid))
	for kind, count := range c.page.canvasInvalid {
		r.Invalid[kind] = count
	}
	return &r
}

// Called while page.mu is held. Grading is unchanged; these labels explain
// rejected synthetic events and are never supplied to the model.
func (f *browserLivePage) recordCanvasPageClick(e browserLivePageEvent, accepted bool) {
	if f.name != "canvas" {
		return
	}
	category := "accepted"
	if !accepted {
		switch {
		case !e.Trusted:
			category = "untrusted"
		case e.Kind != "canvas":
			category = "unexpected_kind"
		case len(f.events) > 0:
			category = "after_success"
		case e.X < 0 || e.X >= 900 || e.Y < 0 || e.Y >= 600:
			category = "outside_canvas"
		case e.X >= 400 && e.X <= 500 && e.Y >= 280 && e.Y <= 346:
			category = "red_decoy"
		default:
			category = "outside_target"
		}
		if f.canvasInvalid == nil {
			f.canvasInvalid = make(map[string]int)
		}
		f.canvasInvalid[category]++
	}
	if len(f.canvasEvents) < 64 {
		f.canvasEvents = append(f.canvasEvents, browserLiveCanvasPageClick{category, e.Trusted, e.X, e.Y, e.ClientX, e.ClientY, e.CanvasRect, e.Viewport, e.DPR})
	}
}

func TestBrowserLiveCanvasDiagnosticsOffline(t *testing.T) {
	f := newBrowserLivePage("canvas")
	defer f.server.Close()
	f.canvasX, f.canvasY = 200, 150
	trace := &browserLiveTrace{canvas: newBrowserLiveCanvasTrace(f)}
	o := browserhost.Observation{URL: f.address(), TabID: "synthetic", DocumentID: "synthetic-doc", Generation: 1,
		ViewportWidth: 720, ViewportHeight: 480, ScreenshotWidth: 1440, ScreenshotHeight: 960, Screenshot: []byte{1},
		Text: "never-report-this-page-text", Elements: []browserhost.Element{{ID: "canvas-id", Role: "canvas", W: 720, H: 480}}}
	trace.recordCanvasObservation(o)
	// Canvas center (250,183) -> CSS (200,146.4) -> screenshot (400,292.8).
	args := `{"kind":"click","x":400,"y":292.8}`
	trace.recordCanvasDecision(provider.ToolCall{Name: "browser_action", ID: "never-report-tool-id", Arguments: args})
	a, err := browserAction(args, browserObserved{Observation: o}, true)
	if err != nil || math.Abs(a.X-200) > 1e-6 || math.Abs(a.Y-146.4) > 1e-6 {
		t.Fatal("screenshot to CSS mapping mismatch")
	}
	trace.recordCanvasAction(a, nil)
	e := browserLivePageEvent{Kind: "canvas", Trusted: true, X: 250, Y: 183, ClientX: a.X, ClientY: a.Y,
		CanvasRect: browserLiveRect{Width: 720, Height: 480}, Viewport: browserLiveSize{720, 480}, DPR: 2}
	f.mu.Lock()
	f.recordCanvasPageClick(e, f.accepts(e))
	f.events = append(f.events, e)
	f.recordCanvasPageClick(e, false)
	f.mu.Unlock()
	var r browserLiveCaseReport
	trace.copyTo(&r)
	d := r.CanvasDiagnostics
	if d == nil || len(d.Views) != 1 || len(d.Actions) != 1 || len(d.Decisions) != 1 || len(d.PageClicks) != 2 || d.Invalid["after_success"] != 1 || d.Views[0].TargetCSS != (browserLiveRect{160, 120, 80, 52.8}) {
		t.Fatal("synthetic canvas diagnostics missing or inconsistent")
	}
	body, _ := json.Marshal(r)
	if strings.Contains(string(body), "never-report") || strings.Contains(string(body), "canvas-id") || strings.Contains(string(body), f.address()) {
		t.Fatal("report retained page content or identifiers")
	}
	for _, tt := range []struct {
		e    browserLivePageEvent
		want string
	}{
		{browserLivePageEvent{Kind: "canvas", X: 450, Y: 310, Trusted: true}, "red_decoy"},
		{browserLivePageEvent{Kind: "canvas", X: 20, Y: 20, Trusted: true}, "outside_target"},
		{browserLivePageEvent{Kind: "canvas", X: 1000, Y: 20, Trusted: true}, "outside_canvas"},
		{browserLivePageEvent{Kind: "canvas", X: 250, Y: 183}, "untrusted"},
		{browserLivePageEvent{Kind: "never-report-kind", Trusted: true}, "unexpected_kind"},
	} {
		p := &browserLivePage{name: "canvas", canvasX: 200, canvasY: 150}
		p.recordCanvasPageClick(tt.e, false)
		if p.canvasInvalid[tt.want] != 1 {
			t.Fatalf("missing safe rejection category %s", tt.want)
		}
	}
	trace.recordCanvasObservation(browserhost.Observation{URL: "https://not-the-fixture.invalid"})
	trace.recordCanvasDecision(provider.ToolCall{Name: "browser_action", Arguments: args})
	trace.recordCanvasAction(a, nil)
	trace.copyTo(&r)
	if len(r.CanvasDiagnostics.Decisions) != 1 || len(r.CanvasDiagnostics.Actions) != 1 {
		t.Fatal("coordinates recorded outside the synthetic fixture")
	}
}
