package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"45s", 45 * time.Second, false},
		{"4m", 4 * time.Minute, false},
		{"24h", 24 * time.Hour, false},
		{"2d", 48 * time.Hour, false},
		{"0d", 0, false},
		{"1h30m", 90 * time.Minute, false},
		{"5x", 0, true},
		{"d", 0, true},
		{"-1d", 0, true},
		{"1.5d", 0, true},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseAge(tt.in)
			if (err != nil) != tt.err {
				t.Fatalf("parseAge(%q) err = %v, wantErr %v", tt.in, err, tt.err)
			}
			if !tt.err && got != tt.want {
				t.Errorf("parseAge(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestMaybeRemoveExpired(t *testing.T) {
	const oldTS = "2020-01-01 00:00:00 UTC"
	freshTS := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

	writeTmp := func(t *testing.T) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "in.yaml")
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exists := func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}

	t.Run("empty maxAge keeps file", func(t *testing.T) {
		p := writeTmp(t)
		if err := maybeRemoveExpired(p, oldTS, ""); err != nil {
			t.Fatal(err)
		}
		if !exists(p) {
			t.Error("file should remain when maxAge is empty")
		}
	})

	t.Run("removes expired file", func(t *testing.T) {
		p := writeTmp(t)
		if err := maybeRemoveExpired(p, oldTS, "1h"); err != nil {
			t.Fatal(err)
		}
		if exists(p) {
			t.Error("file should be removed when older than maxAge")
		}
	})

	t.Run("keeps fresh file", func(t *testing.T) {
		p := writeTmp(t)
		if err := maybeRemoveExpired(p, freshTS, "24h"); err != nil {
			t.Fatal(err)
		}
		if !exists(p) {
			t.Error("fresh file should not be removed")
		}
	})

	t.Run("empty createdAt skips with no error", func(t *testing.T) {
		p := writeTmp(t)
		if err := maybeRemoveExpired(p, "", "1h"); err != nil {
			t.Fatal(err)
		}
		if !exists(p) {
			t.Error("file should be kept when createdAt is empty")
		}
	})

	t.Run("invalid maxAge errors", func(t *testing.T) {
		p := writeTmp(t)
		if err := maybeRemoveExpired(p, oldTS, "5x"); err == nil {
			t.Error("expected error for invalid maxAge")
		}
		if !exists(p) {
			t.Error("file should remain when maxAge fails to parse")
		}
	})

	t.Run("invalid createdAt errors", func(t *testing.T) {
		p := writeTmp(t)
		if err := maybeRemoveExpired(p, "not-a-date", "1h"); err == nil {
			t.Error("expected error for invalid createdAt")
		}
		if !exists(p) {
			t.Error("file should remain when createdAt fails to parse")
		}
	})
}
