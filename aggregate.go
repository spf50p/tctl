package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
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
	var (
		inputPath  string
		outputPath string
		dbPath     string
		maxAge     string
	)
	cmd := &cobra.Command{
		Use:     "aggregate",
		Aliases: []string{"a"},
		Short:   "group IPs from collected_ips.yaml by country/city into aggregated_geo.json",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runAggregate(inputPath, outputPath, dbPath, maxAge)
		},
	}
	cmd.Flags().StringVarP(&inputPath, "input", "i", "collected_ips.yaml", "input path")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "aggregated_geo.json", "output path")
	cmd.Flags().StringVar(&dbPath, "db", "mmdb/GeoLite2-City.mmdb", "mmdb path")
	cmd.Flags().StringVar(&maxAge, "max-age", "", "delete input file when its created_at is older than this (e.g. 45s/4m/24h/2d)")
	return cmd
}

func parseAge(s string) (time.Duration, error) {
	if num, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(num)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func runAggregate(inputPath, outputPath, dbPath, maxAge string) error {
	b, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", inputPath, err)
	}
	var in ipList
	if err := yaml.Unmarshal(b, &in); err != nil {
		return fmt.Errorf("parse %s: %w", inputPath, err)
	}

	db, err := maxminddb.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open mmdb %s: %w", dbPath, err)
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
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Printf("wrote %d entries to %s\n", len(out), outputPath)

	return maybeRemoveExpired(inputPath, in.CreatedAt, maxAge)
}

func maybeRemoveExpired(inputPath, createdAt, maxAge string) error {
	if maxAge == "" {
		return nil
	}
	age, err := parseAge(maxAge)
	if err != nil {
		return fmt.Errorf("parse --max-age: %w", err)
	}
	if createdAt == "" {
		fmt.Fprintf(os.Stderr, "max-age check skipped: %s has no created_at\n", inputPath)
		return nil
	}
	created, err := time.Parse("2006-01-02 15:04:05 UTC", createdAt)
	if err != nil {
		return fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	if time.Since(created) < age {
		return nil
	}
	if err := os.Remove(inputPath); err != nil {
		return fmt.Errorf("remove %s: %w", inputPath, err)
	}
	fmt.Printf("removed %s (age >= %s)\n", inputPath, maxAge)
	return nil
}
