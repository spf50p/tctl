package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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

func writeConfig(t *testing.T, dir, baseURL, token string) string {
	t.Helper()
	p := filepath.Join(dir, "cfg.yaml")
	body := fmt.Sprintf("telemt_servers:\n  - base_url: %s\n    token: %s\n", baseURL, token)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
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
	cfg := writeConfig(t, dir, srv.URL, "test-token")
	out := filepath.Join(dir, "out.yaml")

	if err := runCollect(cfg, out); err != nil {
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
	cfg := writeConfig(t, dir, srv.URL, "test-token")
	out := filepath.Join(dir, "out.yaml")

	existing := "unique_ips_list:\n  - 1.1.1.1\ncreated_at: 2020-01-01 00:00:00 UTC\nlast_update: 2020-01-01 00:00:00 UTC\n"
	if err := os.WriteFile(out, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runCollect(cfg, out); err != nil {
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
}

func TestRunCollect_NonOKResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"ok":false,"data":[]}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := writeConfig(t, dir, srv.URL, "test-token")
	out := filepath.Join(dir, "out.yaml")

	if err := runCollect(cfg, out); err != nil {
		t.Fatalf("runCollect: %v", err)
	}
	b, _ := os.ReadFile(out)
	var got ipList
	yaml.Unmarshal(b, &got)
	if len(got.UniqueIPsList) != 0 {
		t.Errorf("UniqueIPsList = %v, want empty", got.UniqueIPsList)
	}
	if got.CreatedAt == "" {
		t.Error("CreatedAt should still be set even with empty list")
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
