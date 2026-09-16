package visioncap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/billing"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/product"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const (
	Supported           = "supported"
	Unsupported         = "unsupported"
	Unknown             = "unknown"
	Probing             = "probing"
	OverrideAuto        = "auto"
	SourceProbe         = "probe"
	SourceMetadata      = "metadata"
	SourceManual        = "manual"
	CurrentProbeVersion = 3
)

type Capability struct {
	ModelRef        string `json:"modelRef"`
	Key             string `json:"key"`
	Status          string `json:"status"`
	AutomaticStatus string `json:"automaticStatus,omitempty"`
	CheckedAt       int64  `json:"checkedAt,omitempty"`
	Reason          string `json:"reason,omitempty"`
	Attempts        int    `json:"attempts,omitempty"`
	Source          string `json:"source,omitempty"`
	Override        string `json:"override,omitempty"`
	ProbeVersion    int    `json:"probeVersion,omitempty"`
}

type Store struct {
	mu    sync.Mutex
	path  string
	Items map[string]Capability `json:"items"`
}

// Store instances are intentionally cheap and are loaded independently by the
// desktop settings and boot paths. Serialize file updates across those
// instances so parallel model probes cannot overwrite each other's result.
var storeFileMu sync.Mutex

func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "."+product.ConfigDirName)
	} else {
		dir = filepath.Join(dir, product.ConfigDirName)
	}
	return filepath.Join(dir, "vision-capabilities.json")
}

func Load(path string) *Store {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	s := &Store{path: path, Items: map[string]Capability{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, s)
		s.path = path
	}
	if s.Items == nil {
		s.Items = map[string]Capability{}
	}
	for key, item := range s.Items {
		if item.Status == Probing {
			item.Status = Unknown
			item.Reason = "previous vision probe was interrupted"
			s.Items[key] = item
		}
		if item.Source == SourceProbe && item.ProbeVersion < CurrentProbeVersion {
			item.Status = Unknown
			item.Reason = "vision probe needs refresh"
			item.Attempts = 0
			s.Items[key] = item
		}
	}
	return s
}

func Key(e *config.ProviderEntry) string {
	if e == nil {
		return ""
	}
	base := strings.TrimRight(strings.ToLower(strings.TrimSpace(e.BaseURL)), "/")
	return strings.ToLower(strings.TrimSpace(e.Kind)) + "|" + base + "|" + strings.TrimSpace(e.Model)
}

func ModelRef(e *config.ProviderEntry) string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Name) + "/" + strings.TrimSpace(e.Model)
}

func (s *Store) Get(e *config.ProviderEntry) Capability {
	c := s.Stored(e)
	c.AutomaticStatus = c.Status
	if c.Override == Supported || c.Override == Unsupported {
		c.Status = c.Override
		c.Source = SourceManual
	}
	return c
}

// Stored returns the last automatic result without applying a manual override.
// Callers that update or clear the override need this underlying value.
func (s *Store) Stored(e *config.ProviderEntry) Capability {
	k := Key(e)
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.Items[k]; ok {
		c.Key = k
		c.ModelRef = ModelRef(e)
		if status, known := officialModelStatus(e); known {
			c.Status = status
			c.AutomaticStatus = status
			c.Source = SourceMetadata
			c.Reason = "DeepSeek official model capability"
			if status == Supported {
				c.Override = s.officialFlashOverride(e, c.Override)
			}
		}
		if c.AutomaticStatus == "" {
			c.AutomaticStatus = c.Status
		}
		return c
	}
	if status, known := officialModelStatus(e); known {
		c := Capability{ModelRef: ModelRef(e), Key: k, Status: status, AutomaticStatus: status, Source: SourceMetadata, Reason: "DeepSeek official model capability", Override: OverrideAuto}
		if status == Supported {
			c.Override = s.officialFlashOverride(e, c.Override)
		}
		return c
	}
	return Capability{ModelRef: ModelRef(e), Key: k, Status: Unknown, Override: OverrideAuto}
}

// Called under s.mu only for an official Flash entry. Keep its endpoint and
// storage key intact so clearing an inherited override writes the requested ID.
func (s *Store) officialFlashOverride(e *config.ProviderEntry, fallback string) string {
	alias := *e
	for _, model := range []string{"deepseek-flash", e.Model} {
		if !strings.EqualFold(strings.TrimSpace(model), "deepseek-flash") {
			continue
		}
		alias.Model = model
		switch override := s.Items[Key(&alias)].Override; override {
		case Supported, Unsupported, OverrideAuto:
			// Explicit canonical auto also wins, allowing users to clear an override.
			return override
		}
	}
	for _, model := range []string{e.Model, "deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
		alias.Model = model
		switch s.Items[Key(&alias)].Override {
		case Unsupported:
			return Unsupported
		case Supported:
			fallback = Supported
		}
	}
	return fallback
}

func officialModelStatus(e *config.ProviderEntry) (string, bool) {
	if e == nil || !strings.EqualFold(strings.TrimSpace(e.Kind), "openai") {
		return "", false
	}
	switch billing.OfficialDeepSeekModel(e.Name, e.BaseURL, e.Model) {
	case "deepseek-v4-pro":
		return Unsupported, true
	case "deepseek-flash":
		return Supported, true
	default:
		return "", false
	}
}

func (s *Store) Put(c Capability) error {
	storeFileMu.Lock()
	defer storeFileMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Items == nil {
		s.Items = map[string]Capability{}
	}
	if b, err := os.ReadFile(s.path); err == nil {
		var disk struct {
			Items map[string]Capability `json:"items"`
		}
		if json.Unmarshal(b, &disk) == nil {
			for key, item := range disk.Items {
				s.Items[key] = item
			}
		}
	}
	s.Items[c.Key] = c
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(struct {
		Items map[string]Capability `json:"items"`
	}{s.Items}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) List(cfg *config.Config) []Capability {
	if cfg == nil {
		return nil
	}
	var out []Capability
	for i := range cfg.Providers {
		for _, model := range cfg.Providers[i].ModelList() {
			e := cfg.Providers[i]
			e.Model = model
			out = append(out, s.Get(&e))
		}
	}
	return out
}

// Probe sends a generated high-contrast digit image. It is intentionally
// independent of desktop/Wails so fake providers can test every outcome.
func Probe(ctx context.Context, p provider.Provider, e *config.ProviderEntry, iconPNG []byte) Capability {
	code, data, err := probeImage(iconPNG)
	c := Capability{ModelRef: ModelRef(e), Key: Key(e), Status: Unknown, CheckedAt: time.Now().UnixMilli(), Source: SourceProbe, ProbeVersion: CurrentProbeVersion}
	if err != nil {
		c.Reason = err.Error()
		return c
	}
	return probeWithImage(ctx, p, e, code, data)
}

func probeWithImage(ctx context.Context, p provider.Provider, e *config.ProviderEntry, code, data string) Capability {
	c := Capability{ModelRef: ModelRef(e), Key: Key(e), Status: Unknown, CheckedAt: time.Now().UnixMilli(), Source: SourceProbe, Override: OverrideAuto, ProbeVersion: CurrentProbeVersion}
	req := provider.Request{Temperature: 0, MaxTokens: 1024, Messages: []provider.Message{{Role: provider.RoleUser, Content: "Inspect the attached Orca test image. Report the four-digit code, the color of the large marker, and its position. Reply as: CODE COLOR POSITION.", Images: []provider.ImageContent{{Name: "orca-vision-probe.png", MediaType: "image/png", Data: data}}}}}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		c.Status = statusForProbeError(err)
		c.AutomaticStatus = c.Status
		c.Reason = summarizeError(err)
		return c
	}
	var visible strings.Builder
	var reasoning strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkText {
			visible.WriteString(chunk.Text)
		}
		if chunk.Type == provider.ChunkReasoning {
			reasoning.WriteString(chunk.Text)
		}
		if chunk.Type == provider.ChunkError && chunk.Err != nil {
			c.Status = statusForProbeError(chunk.Err)
			c.AutomaticStatus = c.Status
			c.Reason = summarizeError(chunk.Err)
			return c
		}
	}
	answer := strings.TrimSpace(visible.String())
	if probeAnswerMatches(answer, code) {
		c.Status = Supported
		c.AutomaticStatus = Supported
		return c
	}
	if answer == "" && probeAnswerMatches(reasoning.String(), code) {
		c.Status = Supported
		c.AutomaticStatus = Supported
		return c
	}
	if explicitlyRejectsImageAnswer(answer) {
		c.Status = Unsupported
		c.AutomaticStatus = Unsupported
		c.Reason = "model explicitly stated that it cannot inspect images"
		return c
	}
	c.Status = Unknown
	c.AutomaticStatus = Unknown
	if answer == "" {
		c.Reason = "probe completed without a verifiable final answer"
	} else {
		c.Reason = "probe response did not contain the test code"
	}
	return c
}

func explicitlyRejectsImageAnswer(answer string) bool {
	message := strings.ToLower(strings.TrimSpace(answer))
	if message == "" {
		return false
	}
	mentionsImage := strings.Contains(message, "image") || strings.Contains(message, "vision") || strings.Contains(message, "图片") || strings.Contains(message, "图像")
	rejects := strings.Contains(message, "cannot") || strings.Contains(message, "can't") || strings.Contains(message, "unable") || strings.Contains(message, "not support") || strings.Contains(message, "无法") || strings.Contains(message, "不支持")
	return mentionsImage && rejects
}

func probeAnswerMatches(answer, code string) bool {
	answer = strings.TrimSpace(answer)
	if strings.Contains(code, "|") {
		normalized := strings.ToUpper(answer)
		for _, part := range strings.Split(code, "|") {
			part = strings.TrimSpace(strings.ToUpper(part))
			if part != "" && !strings.Contains(normalized, part) {
				return false
			}
		}
		return true
	}
	if answer == code {
		return true
	}
	// Providers occasionally wrap a correct short answer in punctuation or a
	// sentence despite the digits-only instruction. Require a standalone match
	// so unrelated numbers in a refusal cannot produce a false positive.
	for i := 0; i+len(code) <= len(answer); i++ {
		if answer[i:i+len(code)] != code {
			continue
		}
		leftDigit := i > 0 && answer[i-1] >= '0' && answer[i-1] <= '9'
		right := i + len(code)
		rightDigit := right < len(answer) && answer[right] >= '0' && answer[right] <= '9'
		if !leftDigit && !rightDigit {
			return true
		}
	}
	return false
}

func statusForProbeError(err error) string {
	if err == nil {
		return Unknown
	}
	message := strings.ToLower(err.Error())
	transientOrAccess := []string{"401", "403", "408", "409", "429", "5xx", "timeout", "timed out", "rate limit", "quota", "network", "connection", "authentication", "unauthorized", "forbidden"}
	for _, marker := range transientOrAccess {
		if strings.Contains(message, marker) {
			return Unknown
		}
	}
	mentionsImage := strings.Contains(message, "image") || strings.Contains(message, "vision") || strings.Contains(message, "multimodal")
	explicitRejection := strings.Contains(message, "not support") || strings.Contains(message, "unsupported") || strings.Contains(message, "doesn't support") || strings.Contains(message, "does not accept") || strings.Contains(message, "invalid content type") || strings.Contains(message, "text only") || strings.Contains(message, "no endpoints found")
	if mentionsImage && explicitRejection {
		return Unsupported
	}
	return Unknown
}

func summarizeError(err error) string {
	if err == nil {
		return "probe failed"
	}
	s := strings.TrimSpace(err.Error())
	if len(s) > 240 {
		s = s[:240]
	}
	return s
}

var digitRows = [10][7]string{
	{"111", "101", "101", "101", "101", "101", "111"}, {"010", "110", "010", "010", "010", "010", "111"},
	{"111", "001", "001", "111", "100", "100", "111"}, {"111", "001", "001", "111", "001", "001", "111"},
	{"101", "101", "101", "111", "001", "001", "001"}, {"111", "100", "100", "111", "001", "001", "111"},
	{"111", "100", "100", "111", "101", "101", "111"}, {"111", "001", "001", "010", "010", "100", "100"},
	{"111", "101", "101", "111", "101", "101", "111"}, {"111", "101", "101", "111", "001", "001", "111"},
}

func probeImage(iconPNG []byte) (string, string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	code := fmt.Sprintf("%d%d%d%d", raw[0]%10, raw[1]%10, raw[2]%10, raw[3]%10)
	colors := []struct {
		name  string
		value color.RGBA
	}{{"BLUE", color.RGBA{37, 99, 235, 255}}, {"RED", color.RGBA{220, 38, 38, 255}}, {"GREEN", color.RGBA{22, 163, 74, 255}}, {"PURPLE", color.RGBA{147, 51, 234, 255}}}
	positions := []struct {
		name string
		x, y int
	}{{"TOP-LEFT", 22, 22}, {"TOP-RIGHT", 378, 22}, {"BOTTOM-LEFT", 22, 218}, {"BOTTOM-RIGHT", 378, 218}}
	markerColor := colors[int(raw[4])%len(colors)]
	markerPosition := positions[int(raw[5])%len(positions)]
	challenge := code + "|" + markerColor.name + "|" + markerPosition.name
	img := image.NewRGBA(image.Rect(0, 0, 480, 320))
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{15, 23, 42, 255}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, white)
		}
	}
	if len(iconPNG) > 0 {
		icon, _, decodeErr := image.Decode(bytes.NewReader(iconPNG))
		if decodeErr != nil {
			return "", "", fmt.Errorf("decode probe icon: %w", decodeErr)
		}
		src := icon.Bounds()
		const iconSize = 132
		const iconX = (480 - iconSize) / 2
		const iconY = 18
		for y := 0; y < iconSize; y++ {
			for x := 0; x < iconSize; x++ {
				sx := src.Min.X + x*src.Dx()/iconSize
				sy := src.Min.Y + y*src.Dy()/iconSize
				img.Set(iconX+x, iconY+y, icon.At(sx, sy))
			}
		}
	}
	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			img.Set(markerPosition.x+x, markerPosition.y+y, markerColor.value)
		}
	}
	for i, ch := range code {
		d := int(ch - '0')
		ox := 104 + i*70
		for row, bits := range digitRows[d] {
			for col, bit := range bits {
				if bit != '1' {
					continue
				}
				for yy := 0; yy < 14; yy++ {
					for xx := 0; xx < 14; xx++ {
						img.Set(ox+col*14+xx, 190+row*14+yy, black)
					}
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", "", err
	}
	return challenge, base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
