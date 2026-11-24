//go:build windows && production && browserui

package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Offline verification only: reads test-owned PNGs and reports. It opens no
// window and makes no network request. Requiring the fixture's unique color
// catches blank native captures and native content leaking through a modal.
func TestBetaNativeEvidence(t *testing.T) {
	root := os.Getenv("ORCA_BETA_NATIVE_EVIDENCE")
	if root == "" {
		t.Skip("set ORCA_BETA_NATIVE_EVIDENCE to a completed native matrix root")
	}
	_, source, _, _ := runtime.Caller(0)
	allowed := filepath.Join(filepath.Dir(source), "..", ".tmp")
	rel, err := filepath.Rel(allowed, root)
	if !filepath.IsAbs(root) || err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatal("evidence must be inside this worktree's .tmp")
	}
	if err := validateBetaDirectory(root); err != nil {
		t.Fatal(err)
	}
	stages := []string{"browser", "account", "signing-in", "web-login", "web-closed", "web-settings", "primary-restored"}
	var cases []map[string]any
	totalClicks, totalImages := 0, 0
	for _, style := range []string{"modern", "classic"} {
		for _, language := range []string{"en", "zh"} {
			for _, width := range []int{760, 1024, 1366, 1920} {
				name := fmt.Sprintf("%s-%s-%d", style, language, width)
				data, err := os.ReadFile(filepath.Join(root, name, "report.json"))
				if err != nil {
					t.Fatal(err)
				}
				var report struct {
					Checkpoints  []string `json:"checkpoints"`
					NativeClicks int      `json:"nativeClicks"`
					WindowClass  string   `json:"windowClass"`
					PID          int      `json:"pid"`
				}
				if err := json.Unmarshal(data, &report); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(report.Checkpoints, stages) || report.NativeClicks < 10 || report.WindowClass != betaWindowClass || report.PID <= 0 {
					t.Fatalf("incomplete native case %s: %+v", name, report)
				}
				log, err := os.ReadFile(filepath.Join(root, name, "test.log"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(log), "--- PASS: TestBetaConnectionsInProductionUI") || strings.Contains(string(log), "--- FAIL:") {
					t.Fatalf("case %s has no passing native test log", name)
				}
				var images []map[string]any
				for _, stage := range stages {
					file, err := os.Open(filepath.Join(root, name, stage+".png"))
					if err != nil {
						t.Fatal(err)
					}
					pixels, err := png.Decode(file)
					file.Close()
					if err != nil {
						t.Fatal(err)
					}
					bounds := pixels.Bounds()
					if bounds.Dx() < 700 || bounds.Dy() < 800 {
						t.Fatalf("implausibly small full-window capture %s/%s: %v", name, stage, bounds)
					}
					fixturePixels := betaFixturePixels(pixels)
					wantVisible := stage == "browser" || stage == "web-login" || stage == "primary-restored"
					if wantVisible && fixturePixels < 50 || !wantVisible && fixturePixels != 0 {
						t.Errorf("native painting/occlusion %s/%s: fixture color samples=%d expected visible=%v", name, stage, fixturePixels, wantVisible)
					}
					images = append(images, map[string]any{"stage": stage, "path": name + "/" + stage + ".png", "width": bounds.Dx(), "height": bounds.Dy(), "fixtureColorSamples": fixturePixels, "nativeVisible": wantVisible})
				}
				totalClicks += report.NativeClicks
				totalImages += len(images)
				cases = append(cases, map[string]any{"case": name, "nativeClicks": report.NativeClicks, "windowClass": report.WindowClass, "pid": report.PID, "screenshots": images})
			}
		}
	}
	result, err := json.MarshalIndent(map[string]any{"suite": "beta-native-production-ui", "passed": !t.Failed(), "caseCount": len(cases), "checkpointCount": totalImages, "nativeClicks": totalClicks, "cases": cases, "scope": "production frontend/App bindings, isolated loopback native fixtures; no live account or DeepSeek adapter validation"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "matrix-report.json"), result, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d cases, %d checkpoints/screenshots, %d trusted native clicks; PNG verification passed=%v", len(cases), totalImages, totalClicks, !t.Failed())
}

func betaFixturePixels(pixels image.Image) int {
	bounds, count := pixels.Bounds(), 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 2 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 2 {
			r, g, b, a := pixels.At(x, y).RGBA()
			if r>>8 == 0xe6 && g>>8 == 0xf4 && b>>8 == 0xee && a == 0xffff {
				count++
			}
		}
	}
	return count
}
