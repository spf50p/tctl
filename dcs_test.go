package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDcs(t *testing.T) {
	const body = `{
  "ok": true,
  "data": {
    "middle_proxy_enabled": true,
    "reason": null,
    "generated_at_epoch_secs": 1763212200,
    "dcs": [
      {
        "dc": 2,
        "endpoints": ["1.2.3.4:443", "1.2.3.5:443", "1.2.3.6:443"],
        "available_endpoints": 3,
        "available_pct": 100.0,
        "required_writers": 12,
        "floor_min": 10,
        "floor_target": 12,
        "floor_max": 15,
        "floor_capped": false,
        "alive_writers": 12,
        "coverage_pct": 100.0,
        "fresh_alive_writers": 11,
        "fresh_coverage_pct": 91.7,
        "rtt_ms": 45.2,
        "load": 128
      },
      {
        "dc": 4,
        "endpoints": ["5.6.7.8:443", "5.6.7.9:443"],
        "available_endpoints": 1,
        "available_pct": 50.0,
        "required_writers": 8,
        "floor_min": 8,
        "floor_target": 10,
        "floor_max": 10,
        "floor_capped": true,
        "alive_writers": 6,
        "coverage_pct": 75.0,
        "fresh_alive_writers": 5,
        "fresh_coverage_pct": 62.5,
        "rtt_ms": null,
        "load": 42
      }
    ]
  },
  "revision": "abc"
}`
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		fmt.Fprintln(w, body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	cfgBody := fmt.Sprintf("collect_file_path: /tmp/x.yaml\ntelemt_servers:\n  - base_url: %s\n    token: test-token\n", srv.URL)
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runDcs(cfgPath, nil, &buf); err != nil {
		t.Fatalf("runDcs: %v", err)
	}

	if gotPath != "/v1/stats/dcs" {
		t.Errorf("path = %q, want /v1/stats/dcs", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want %q", gotAuth, "Bearer test-token")
	}
	out := buf.String()
	for _, want := range []string{
		"== " + srv.URL + " ==",
		"middle_proxy_enabled: true",
		"generated_at:",
		"DC", "EP", "AVAIL%", "WRITERS", "COV%", "FRESH", "F-COV%", "FLOOR", "RTT(ms)", "LOAD",
		// DC 2 row
		"2", "3/3", "100.0", "12/12", "11/12", "45.2", "128",
		// DC 4 row with capped floor and null RTT
		"4", "1/2", "50.0", "6/8", "75.0", "5/8", "62.5", "10*", "-", "42",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestFilterServersByTag(t *testing.T) {
	all := []server{
		{BaseURL: "a", Tags: []string{"nl", "primary"}},
		{BaseURL: "b", Tags: []string{"nl2"}},
		{BaseURL: "c", Tags: []string{"de"}},
		{BaseURL: "d"},
	}
	cases := []struct {
		name   string
		filter []string
		want   []string
	}{
		{"empty filter returns all", nil, []string{"a", "b", "c", "d"}},
		{"single match", []string{"nl"}, []string{"a"}},
		{"multiple OR-matches", []string{"nl", "nl2"}, []string{"a", "b"}},
		{"no match", []string{"jp"}, nil},
		{"server without tags never matches when filter set", []string{"primary", "de"}, []string{"a", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterServersByTag(all, tc.filter)
			var urls []string
			for _, s := range got {
				urls = append(urls, s.BaseURL)
			}
			if !equalSlices(urls, tc.want) {
				t.Errorf("filterServersByTag(%v) = %v, want %v", tc.filter, urls, tc.want)
			}
		})
	}
}

func TestRunDcs_TagFilter(t *testing.T) {
	const body = `{"ok":true,"data":{"middle_proxy_enabled":true,"reason":null,"generated_at_epoch_secs":1,"dcs":[]}}`
	srvNL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, body)
	}))
	defer srvNL.Close()
	srvDE := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("srvDE should have been filtered out")
		fmt.Fprintln(w, body)
	}))
	defer srvDE.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	cfgBody := fmt.Sprintf("collect_file_path: /tmp/x.yaml\ntelemt_servers:\n  - base_url: %s\n    token: t\n    tags: [nl]\n  - base_url: %s\n    token: t\n    tags: [de]\n", srvNL.URL, srvDE.URL)
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runDcs(cfgPath, []string{"nl"}, &buf); err != nil {
		t.Fatalf("runDcs: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "== "+srvNL.URL+" ==") {
		t.Errorf("expected NL server in output\n%s", out)
	}
	if strings.Contains(out, srvDE.URL) {
		t.Errorf("DE server should not appear in output\n%s", out)
	}
}

func TestRunDcs_TagFilterNoMatch(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	cfgBody := "collect_file_path: /tmp/x.yaml\ntelemt_servers:\n  - base_url: http://127.0.0.1:9\n    token: t\n    tags: [nl]\n"
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runDcs(cfgPath, []string{"jp"}, &buf); err == nil {
		t.Error("expected error when no servers match the tag filter")
	}
}

func TestRunDcs_MultipleServers(t *testing.T) {
	const body = `{"ok":true,"data":{"middle_proxy_enabled":true,"reason":null,"generated_at_epoch_secs":1,"dcs":[]}}`
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, body)
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv2.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	cfgBody := fmt.Sprintf("collect_file_path: /tmp/x.yaml\ntelemt_servers:\n  - base_url: %s\n    token: t\n  - base_url: %s\n    token: t\n", srv1.URL, srv2.URL)
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runDcs(cfgPath, nil, &buf); err != nil {
		t.Fatalf("runDcs: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "== "+srv1.URL+" ==") {
		t.Errorf("missing header for srv1\n%s", out)
	}
	if !strings.Contains(out, "== "+srv2.URL+" ==") {
		t.Errorf("missing header for srv2\n%s", out)
	}
	if !strings.Contains(out, "error: status 500") {
		t.Errorf("expected error from srv2\n%s", out)
	}
	if !strings.Contains(out, "(no DCs)") {
		t.Errorf("expected (no DCs) for srv1\n%s", out)
	}
}
