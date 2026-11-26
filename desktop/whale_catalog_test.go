package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWhaleCatalogRotatingGenerationAndAccountBoundary(t *testing.T) {
	body := `{"data":[{"id":"lens-main","display_name":"weight-1","generation":1,"context_length":200000,"vision":true}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Error("unexpected request")
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()
	get := func() whaleCatalog {
		c, e := fetchWhaleCatalog(srv.Client(), srv.URL+"/v1", "synthetic")
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	old := get()
	publishWhaleCatalog(srv.URL, "synthetic", old)
	if loadedWhaleCatalog(srv.URL, "other") != nil {
		t.Fatal("owner changed")
	}
	body = `{"data":[{"id":"lens-main","display_name":"weight-2","generation":2,"context_length":64000,"vision":false}]}`
	now := get()
	if !sameModelSet(old.ids(), now.ids()) || now[0].Display != "weight-2" || now[0].Generation != 2 || *now[0].Vision || now[0].Context != 64000 {
		t.Fatal("lost metadata-only update")
	}
	publishWhaleCatalog(srv.URL, "synthetic", now)
	for _, bad := range []string{`{}`, `{"data":null}`, `{"data":[`, `{"data":[{"id":"a"},{"id":"a"}]}`} {
		body = bad
		if _, err := fetchWhaleCatalog(srv.Client(), srv.URL+"/v1", "synthetic"); err == nil {
			t.Fatal("accepted malformed catalog")
		}
	}
	if loadedWhaleCatalog(srv.URL, "synthetic")[0].Generation != 2 {
		t.Fatal("lost last good catalog")
	}
	body = `{"data":[]}`
	if c := get(); c == nil || len(c) != 0 {
		t.Fatal("empty catalog must hide unavailable models")
	}
}
