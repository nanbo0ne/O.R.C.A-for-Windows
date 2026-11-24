package orcaaccount

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDesktopAvailabilityIsCredentialFreeAndDoesNotMutateSession(t *testing.T) {
	for _, test := range []struct {
		status int
		want   error
	}{
		{401, nil}, {404, ErrServiceUnavailable}, {410, ErrServiceUnavailable},
		{501, ErrServiceUnavailable}, {200, ErrProtocol}, {302, ErrProtocol},
		{403, ErrAuthorization}, {429, ErrNetwork}, {503, ErrNetwork},
	} {
		store := &memoryStore{data: []byte("preserved synthetic session")}
		c, err := New(Options{Store: store, HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != "GET" || r.URL.String() != Issuer+APIPrefix+"/session" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Fatal("probe carried credentials or changed endpoint")
			}
			return &http.Response{StatusCode: test.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not returned to the UI"))}, nil
		})}})
		if err != nil {
			t.Fatal(err)
		}
		before := c.Snapshot().Generation
		err = c.CheckDesktopService(context.Background())
		if !errors.Is(err, test.want) || c.Snapshot().Generation != before || string(store.data) != "preserved synthetic session" {
			t.Fatal("wrong availability or session mutation", test.status, err)
		}
		c.Close()
	}
}
