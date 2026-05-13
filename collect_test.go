package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestIPLess(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"1.2.3.4", "1.2.3.5", true},
		{"1.2.3.5", "1.2.3.4", false},
		{"1.2.3.4", "1.2.3.4", false},
		{"9.0.0.1", "10.0.0.1", true},
		{"10.0.0.1", "9.0.0.1", false},
		{"::1", "::2", true},
		{"::2", "::1", false},
		{"1.2.3.4", "::1", true},
		{"::1", "1.2.3.4", false},
		{"garbage", "1.2.3.4", false},
		{"1.2.3.4", "garbage", true},
		{"a", "b", true},
	}
	for _, tt := range tests {
		t.Run(tt.a+"<"+tt.b, func(t *testing.T) {
			if got := ipLess(tt.a, tt.b); got != tt.want {
				t.Errorf("ipLess(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func writeConfig(t *testing.T, dir, baseURL, token, collectFilePath string) string {
	t.Helper()
	p := filepath.Join(dir, "cfg.yaml")
	body := fmt.Sprintf("collect_file_path: %s\ntelemt_servers:\n  - base_url: %s\n    token: %s\n", collectFilePath, baseURL, token)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExpandTimePath(t *testing.T) {
	ref := time.Date(2026, 5, 13, 14, 7, 9, 0, time.UTC)
	tests := []struct {
		in, want string
	}{
		{"/var/lib/tctl/%Y/%m/%d/%H.yaml", "/var/lib/tctl/2026/05/13/14.yaml"},
		{"%Y-%m-%d %H:%M:%S", "2026-05-13 14:07:09"},
		{"no placeholders.yaml", "no placeholders.yaml"},
		{"%%Y%%", "%Y%"},
	}
	for _, tt := range tests {
		if got := expandTimePath(tt.in, ref); got != tt.want {
			t.Errorf("expandTimePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRunCollect_FreshFile(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		fmt.Fprintln(w, `{"ok":true,"data":[{"recent_unique_ips_list":["1.1.1.1","8.8.8.8"]},{"recent_unique_ips_list":["1.1.1.1","9.9.9.9"]}]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "out.yaml")
	cfg := writeConfig(t, dir, srv.URL, "test-token", out)

	if err := runCollect(cfg); err != nil {
		t.Fatalf("runCollect: %v", err)
	}
	if gotPath != "/v1/users" {
		t.Errorf("path = %q, want /v1/users", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want %q", gotAuth, "Bearer test-token")
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got ipList
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	wantIPs := []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}
	if !equalSlices(got.UniqueIPsList, wantIPs) {
		t.Errorf("UniqueIPsList = %v, want %v", got.UniqueIPsList, wantIPs)
	}
	if got.Count != len(wantIPs) {
		t.Errorf("Count = %d, want %d", got.Count, len(wantIPs))
	}
	if got.CreatedAt == "" {
		t.Error("CreatedAt should be set on fresh file")
	}
	if got.CreatedAt != got.LastUpdate {
		t.Errorf("on fresh file CreatedAt and LastUpdate should match, got %q vs %q", got.CreatedAt, got.LastUpdate)
	}
}

func TestRunCollect_PreservesCreatedAtAndMerges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"ok":true,"data":[{"recent_unique_ips_list":["2.2.2.2"]}]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "out.yaml")
	cfg := writeConfig(t, dir, srv.URL, "test-token", out)

	existing := "unique_ips_list:\n  - 1.1.1.1\ncreated_at: 2020-01-01 00:00:00 UTC\nlast_update: 2020-01-01 00:00:00 UTC\n"
	if err := os.WriteFile(out, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runCollect(cfg); err != nil {
		t.Fatalf("runCollect: %v", err)
	}

	b, _ := os.ReadFile(out)
	var got ipList
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.CreatedAt != "2020-01-01 00:00:00 UTC" {
		t.Errorf("CreatedAt = %q, want preserved %q", got.CreatedAt, "2020-01-01 00:00:00 UTC")
	}
	if got.LastUpdate == "2020-01-01 00:00:00 UTC" || got.LastUpdate == "" {
		t.Errorf("LastUpdate should be refreshed, got %q", got.LastUpdate)
	}
	want := []string{"1.1.1.1", "2.2.2.2"}
	if !equalSlices(got.UniqueIPsList, want) {
		t.Errorf("UniqueIPsList = %v, want %v", got.UniqueIPsList, want)
	}
	if got.Count != len(want) {
		t.Errorf("Count = %d, want %d", got.Count, len(want))
	}
}

func TestRunCollect_NonOKResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"ok":false,"data":[]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "out.yaml")
	cfg := writeConfig(t, dir, srv.URL, "test-token", out)

	if err := runCollect(cfg); err != nil {
		t.Fatalf("runCollect: %v", err)
	}
	b, _ := os.ReadFile(out)
	var got ipList
	yaml.Unmarshal(b, &got)
	if len(got.UniqueIPsList) != 0 {
		t.Errorf("UniqueIPsList = %v, want empty", got.UniqueIPsList)
	}
	if got.Count != 0 {
		t.Errorf("Count = %d, want 0", got.Count)
	}
	if got.CreatedAt == "" {
		t.Error("CreatedAt should still be set even with empty list")
	}
}

func TestRunCollect_ExpandsPathAndCreatesDirs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"ok":true,"data":[{"recent_unique_ips_list":["1.1.1.1"]}]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	pattern := filepath.Join(dir, "%Y", "%m", "%d", "%H.yaml")
	cfg := writeConfig(t, dir, srv.URL, "test-token", pattern)

	if err := runCollect(cfg); err != nil {
		t.Fatalf("runCollect: %v", err)
	}

	now := time.Now().UTC()
	want := expandTimePath(pattern, now)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected file %q to exist: %v", want, err)
	}
}

func TestRunCollect_MissingCollectFilePath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	body := "telemt_servers:\n  - base_url: http://127.0.0.1:9\n    token: t\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCollect(cfgPath); err == nil {
		t.Error("expected error when collect_file_path is missing")
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
