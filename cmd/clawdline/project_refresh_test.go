package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestProjectRefreshReadsProjectAndSessionsWithoutWriting(t *testing.T) {
	var routes []string
	request := func(route string) (answer, error) {
		routes = append(routes, route)
		switch route {
		case "/v1/places":
			return answer{Status: 200, Body: []byte(`{"places":[{"path":"/fixture/wave","icon":{"accent":"#68C8D8","cells":[["#68C8D8"]]},"setup":{"icon":"override"}}]}`)}, nil
		case "/v1/sessions?parts=list":
			return answer{Status: 200, Body: []byte(`{"sessions":[]}`)}, nil
		}
		t.Fatal("unexpected route", route)
		return answer{}, nil
	}
	var out bytes.Buffer
	if err := refreshProject("/fixture/wave", &out, request); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "#68C8D8 (override)") || !strings.Contains(got, "status: refreshed") {
		t.Fatal(got)
	}
	if len(routes) != 2 || routes[0] != "/v1/places" || routes[1] != "/v1/sessions?parts=list" {
		t.Fatal(routes)
	}
}

func TestProjectRefreshDistinguishesUnknownAndReadFailure(t *testing.T) {
	var out bytes.Buffer
	missing := func(string) (answer, error) { return answer{Status: 200, Body: []byte(`{"places":[]}`)}, nil }
	if err := refreshProject("/fixture/missing", &out, missing); !errors.Is(err, errProjectNotListed) {
		t.Fatalf("unknown: %v", err)
	}
	if out.Len() != 0 {
		t.Fatal("unknown project claimed success")
	}
	broken := func(string) (answer, error) {
		return answer{Status: 503, Body: []byte(`{"error":"icon_store_unavailable","detail":"Project icons could not be read."}`)}, nil
	}
	if err := refreshProject("/fixture/wave", &out, broken); err == nil || !strings.Contains(err.Error(), "icon_store_unavailable") {
		t.Fatalf("read failure: %v", err)
	}
	if out.Len() != 0 {
		t.Fatal("read failure claimed success")
	}
}
