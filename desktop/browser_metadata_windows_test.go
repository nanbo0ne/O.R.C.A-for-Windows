//go:build windows

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrowserMetadataSharingFailureRecovery(t *testing.T) {
	s, _, id := browserFixture(t)
	s.saveMetadata("owner")
	path := s.metadataPath("owner")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	s.mu.Lock()
	s.tabs[id].Title = "Synthetic changed title"
	s.mu.Unlock()
	s.saveMetadata("owner")
	if !strings.HasPrefix(s.state("owner").Error, browserMetadataErrorPrefix) {
		t.Fatal("persistent file obstruction was hidden")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, before) {
		t.Fatal("failed replacement changed the old metadata")
	}
	done := make(chan error, 1)
	go func() {
		time.Sleep(30 * time.Millisecond)
		done <- reader.Close()
	}()
	s.saveMetadata("owner")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.state("owner").Error != "" {
		t.Fatal("recovered save left a stale error")
	}
	got, err = os.ReadFile(path)
	if err != nil || !bytes.Contains(got, []byte("Synthetic changed title")) {
		t.Fatal("replacement did not persist the new metadata")
	}
	s.mu.Lock()
	s.owners["owner"].err = "Navigation needs confirmation"
	s.mu.Unlock()
	s.saveMetadata("owner")
	if s.state("owner").Error != "Navigation needs confirmation" {
		t.Fatal("metadata save cleared an unrelated browser error")
	}
}
