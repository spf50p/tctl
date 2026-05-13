package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/oschwald/maxminddb-golang"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type cityRecord struct {
	Country struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  *float64 `maxminddb:"latitude"`
		Longitude *float64 `maxminddb:"longitude"`
	} `maxminddb:"location"`
}

type aggregateEntry struct {
	Country   *string  `json:"country"`
	City      *string  `json:"city"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Count     int      `json:"count"`
}

func aggregateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "aggregate",
		Aliases: []string{"a"},
		Short:   "group IPs from collect_file_path by country/city into aggregate_file_path",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runAggregate(configPath)
		},
	}
	return cmd
}

func runAggregate(configPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.CollectFilePath == "" {
		return fmt.Errorf("collect_file_path is empty in %s", configPath)
	}
	if cfg.AggregateFilePath == "" {
		return fmt.Errorf("aggregate_file_path is empty in %s", configPath)
	}
	if cfg.MMDBCity == "" {
		return fmt.Errorf("mmdb_city is empty in %s", configPath)
	}

	nowT := time.Now().UTC()
	inputPath := expandTimePath(cfg.CollectFilePath, nowT)
	outputPath := expandTimePath(cfg.AggregateFilePath, nowT)

	b, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", inputPath, err)
	}
	var in ipList
	if err := yaml.Unmarshal(b, &in); err != nil {
		return fmt.Errorf("parse %s: %w", inputPath, err)
	}

	db, err := maxminddb.Open(cfg.MMDBCity)
	if err != nil {
		return fmt.Errorf("open mmdb %s: %w", cfg.MMDBCity, err)
	}
	defer db.Close()

	type key struct {
		country string
		city    string
	}
	agg := make(map[key]*aggregateEntry)

	for _, ipStr := range in.UniqueIPsList {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			fmt.Fprintf(os.Stderr, "skipping invalid IP: %q\n", ipStr)
			continue
		}
		var rec cityRecord
		if err := db.Lookup(ip, &rec); err != nil {
			fmt.Fprintf(os.Stderr, "lookup %s: %v\n", ipStr, err)
			continue
		}
		country := rec.Country.Names["en"]
		city := rec.City.Names["en"]
		k := key{country, city}
		entry, ok := agg[k]
		if !ok {
			entry = &aggregateEntry{}
			if country != "" {
				v := country
				entry.Country = &v
			}
			if city != "" {
				v := city
				entry.City = &v
			}
			entry.Latitude = rec.Location.Latitude
			entry.Longitude = rec.Location.Longitude
			agg[k] = entry
		}
		entry.Count++
	}

	out := make([]*aggregateEntry, 0, len(agg))
	for _, e := range agg {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(outputPath), err)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Printf("wrote %d entries to %s\n", len(out), outputPath)
	return nil
}
