package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type server struct {
	BaseURL string `yaml:"base_url"`
	Token   string `yaml:"token"`
}

type config struct {
	TelemtServers []server `yaml:"telemt_servers"`
}

type usersResponse struct {
	OK   bool       `json:"ok"`
	Data []userData `json:"data"`
}

type userData struct {
	RecentUniqueIPsList []string `json:"recent_unique_ips_list"`
}

type ipList struct {
	UniqueIPsList []string `yaml:"unique_ips_list"`
	Count         int      `yaml:"count"`
	CreatedAt     string   `yaml:"created_at,omitempty"`
	LastUpdate    string   `yaml:"last_update"`
}

func collectCmd() *cobra.Command {
	var outputPath string
	cmd := &cobra.Command{
		Use:     "collect",
		Aliases: []string{"c"},
		Short:   "fetch recent_unique_ips_list from API endpoints into collected_ips.yaml",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCollect(configPath, outputPath)
		},
	}
	cmd.Flags().StringVarP(&outputPath, "output", "o", "collected_ips.yaml", "output path")
	cmd.AddCommand(aggregateCmd())
	return cmd
}

func runCollect(configPath, outputPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfg.TelemtServers) == 0 {
		return fmt.Errorf("no telemt_servers in %s", configPath)
	}

	ips := make(map[string]struct{})
	var createdAt string
	if b, err := os.ReadFile(outputPath); err == nil {
		var existing ipList
		if err := yaml.Unmarshal(b, &existing); err == nil {
			for _, ip := range existing.UniqueIPsList {
				ips[ip] = struct{}{}
			}
			createdAt = existing.CreatedAt
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", outputPath, err)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	if createdAt == "" {
		createdAt = now
	}

	client := &http.Client{Timeout: 30 * time.Second}
	for _, s := range cfg.TelemtServers {
		if s.BaseURL == "" || s.Token == "" {
			fmt.Fprintln(os.Stderr, "skipping incomplete server entry")
			continue
		}
		resp, err := fetchUsers(client, s.BaseURL, s.Token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error fetching %s: %v\n", s.BaseURL, err)
			continue
		}
		if !resp.OK {
			fmt.Fprintf(os.Stderr, "non-ok response from %s\n", s.BaseURL)
			continue
		}
		for _, u := range resp.Data {
			for _, ip := range u.RecentUniqueIPsList {
				ips[ip] = struct{}{}
			}
		}
	}

	sorted := make([]string, 0, len(ips))
	for ip := range ips {
		sorted = append(sorted, ip)
	}
	sort.Slice(sorted, func(i, j int) bool { return ipLess(sorted[i], sorted[j]) })

	out := ipList{
		UniqueIPsList: sorted,
		Count:         len(sorted),
		CreatedAt:     createdAt,
		LastUpdate:    now,
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Printf("wrote %d unique IPs to %s\n", len(sorted), outputPath)
	return nil
}

func loadConfig(path string) (*config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func fetchUsers(client *http.Client, baseURL, token string) (*usersResponse, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/users"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %s: %s", resp.Status, body)
	}
	var ur usersResponse
	if err := json.NewDecoder(resp.Body).Decode(&ur); err != nil {
		return nil, err
	}
	return &ur, nil
}

func ipLess(a, b string) bool {
	ia := net.ParseIP(a)
	ib := net.ParseIP(b)
	if ia == nil && ib == nil {
		return a < b
	}
	if ia == nil {
		return false
	}
	if ib == nil {
		return true
	}
	a4 := ia.To4()
	b4 := ib.To4()
	if a4 != nil && b4 == nil {
		return true
	}
	if a4 == nil && b4 != nil {
		return false
	}
	if a4 != nil {
		ia = a4
	}
	if b4 != nil {
		ib = b4
	}
	return bytes.Compare(ia, ib) < 0
}
