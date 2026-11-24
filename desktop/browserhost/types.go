// Package browserhost defines the isolated embedded-browser boundary. It never
// exposes script evaluation, filesystem access, or the application's Wails bridge.
package browserhost

import (
	"context"
	"errors"
	"runtime"
)

var (
	ErrUnsupported      = errors.New("embedded browser is unavailable on this platform")
	ErrStaleObservation = errors.New("browser observation is stale; observe again")
	ErrNavigationDenied = errors.New("browser navigation requires approval")
)

type Tab struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Loading      bool    `json:"loading"`
	CanGoBack    bool    `json:"canGoBack"`
	CanGoForward bool    `json:"canGoForward"`
	Zoom         float64 `json:"zoom"`
	Error        string  `json:"error"`
}

type Event struct {
	TabID        string    `json:"tabId"`
	Kind         string    `json:"kind"`
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	Error        string    `json:"error"`
	CanGoBack    bool      `json:"canGoBack"`
	CanGoForward bool      `json:"canGoForward"`
	Download     *Download `json:"download,omitempty"`
	Upload       *Upload   `json:"upload,omitempty"`
}

// Upload requests file-selection consent, not permission to run arbitrary CDP.
// selected means the files were given to the page; it does not claim that the
// page submitted them or that a remote server accepted an upload.
type Upload struct {
	ID               string `json:"id"`
	TabID            string `json:"tabId"`
	URL              string `json:"url"`
	Origin           string `json:"origin"`
	DocumentID       string `json:"documentId"`
	Generation       uint64 `json:"generation"`
	FrameID          string `json:"frameId"`
	BackendNodeID    int64  `json:"backendNodeId"`
	Multiple         bool   `json:"multiple"`
	Accept           string `json:"accept"`
	Status           string `json:"status"`
	Error            string `json:"error,omitempty"`
	RequiresApproval bool   `json:"requiresApproval"`
}

// Download is a snapshot, never a grant of filesystem or navigation permission.
// TotalBytes is -1 when the server does not advertise a size. Path is populated
// only after the user explicitly approves a destination through the service.
type Download struct {
	ID       string `json:"id"`
	TabID    string `json:"tabId"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Path     string `json:"path,omitempty"`
	Status   string `json:"status"`
	// Phase finalizing means all known bytes arrived but WebView2 has NOT
	// reported completion. Its API does not expose a SmartScreen scan state.
	Phase            string `json:"phase,omitempty"`
	Error            string `json:"error,omitempty"`
	BytesReceived    int64  `json:"bytesReceived"`
	TotalBytes       int64  `json:"totalBytes"`
	RequiresApproval bool   `json:"requiresApproval"`
}

type Viewport struct {
	TabID   string  `json:"tabId"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Scale   float64 `json:"scale"`
	Visible bool    `json:"visible"`
}

type Observation struct {
	TabID             string    `json:"tabId"`
	URL               string    `json:"url"`
	Title             string    `json:"title"`
	DocumentID        string    `json:"documentId"`
	Generation        uint64    `json:"generation"`
	Text              string    `json:"text"`
	Elements          []Element `json:"elements"`
	ViewportWidth     float64   `json:"viewportWidth"`
	ViewportHeight    float64   `json:"viewportHeight"`
	ScreenshotWidth   int       `json:"screenshotWidth,omitempty"`
	ScreenshotHeight  int       `json:"screenshotHeight,omitempty"`
	ScreenshotBlocked string    `json:"screenshotBlocked,omitempty"`
	Screenshot        []byte    `json:"-"`
	MIME              string    `json:"-"`
}

type Element struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	X         float64        `json:"x"`
	Y         float64        `json:"y"`
	W         float64        `json:"w"`
	H         float64        `json:"h"`
	Value     string         `json:"value"`
	Href      string         `json:"href,omitempty"`
	Sensitive bool           `json:"sensitive,omitempty"`
	Disabled  bool           `json:"disabled,omitempty"`
	Options   []SelectOption `json:"options,omitempty"`
}

type SelectOption struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled"`
}

type Action struct {
	Kind       string  `json:"kind"`
	ElementID  string  `json:"elementId"`
	Text       string  `json:"text"`
	Key        string  `json:"key"`
	URL        string  `json:"url"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	DeltaY     float64 `json:"deltaY"`
	Generation uint64  `json:"generation"`
	DocumentID string  `json:"documentId"`
}

// NavigationGuard is called synchronously before any top-level navigation,
// redirect or popup request. A nil guard denies navigation. Backends must never
// wait for UI approval inside their native callback.
type Backend interface {
	Supported() bool
	SetNavigationGuard(func(tabID, url string) bool)
	Create(context.Context, string, string, string) error
	Close(context.Context, string) error
	Navigate(context.Context, string, string) error
	Command(context.Context, string, string, string) (string, error)
	Present(context.Context, Viewport) error
	Observe(context.Context, string) (Observation, error)
	Execute(context.Context, string, Action) error
	Shutdown()
}

// ShutdownConfirmer acknowledges that no renderer can still read upload files.
// A timeout or a backend without this interface must retain selected snapshots.
type ShutdownConfirmer interface {
	ShutdownConfirmed(context.Context) error
}

// Supported reports platform availability without creating a native engine.
func (h *Host) Supported() bool { return runtime.GOOS == "windows" }
