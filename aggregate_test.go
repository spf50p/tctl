package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunAggregate_MissingConfigFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
	}{
		{"no collect_file_path", "aggregate_file_path: /tmp/out.json\nmmdb_city: /tmp/db.mmdb\n"},
		{"no aggregate_file_path", "collect_file_path: /tmp/in.yaml\nmmdb_city: /tmp/db.mmdb\n"},
		{"no mmdb_city", "collect_file_path: /tmp/in.yaml\naggregate_file_path: /tmp/out.json\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "cfg.yaml")
			if err := os.WriteFile(cfgPath, []byte(tc.cfg), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := runAggregate(cfgPath); err == nil {
				t.Error("expected error for missing required config field")
			}
		})
	}
}
