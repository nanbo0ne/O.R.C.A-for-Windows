package main

import (
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

// The relay may reorder its answer between two pulls; that is not a change, and
// treating it as one would rewrite the config (and poke vision probes) on every
// launch for no reason.
func TestSameModelSet(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"identical", []string{"chimera-1.1"}, []string{"chimera-1.1"}, true},
		{"reordered", []string{"a", "b"}, []string{"b", "a"}, true},
		{"added", []string{"a"}, []string{"a", "b"}, false},
		{"removed", []string{"a", "b"}, []string{"a"}, false},
		{"replaced", []string{"chimera-1"}, []string{"chimera-1.1"}, false},
		{"duplicates differ", []string{"a", "a"}, []string{"a", "b"}, false},
		{"both empty", nil, nil, true},
	}
	for _, c := range cases {
		if got := sameModelSet(c.a, c.b); got != c.want {
			t.Fatalf("%s: sameModelSet(%v, %v) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}

// A model the picker has never seen must survive the chat filter: the whole
// point of the live list is that a swapped local model shows up by its own name.
func TestSwappedModelSurvivesChatFilter(t *testing.T) {
	for _, id := range []string{"chimera-1.1", "chimera", "qwen3.8-27b", "local-deploy"} {
		if !config.IsLikelyChatModel(id) {
			t.Fatalf("model %q would be dropped from the picker", id)
		}
	}
}
