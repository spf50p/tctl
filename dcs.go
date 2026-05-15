package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

type dcStatusResponse struct {
	OK   bool         `json:"ok"`
	Data dcStatusData `json:"data"`
}

type dcStatusData struct {
	MiddleProxyEnabled   bool       `json:"middle_proxy_enabled"`
	Reason               *string    `json:"reason"`
	GeneratedAtEpochSecs int64      `json:"generated_at_epoch_secs"`
	Dcs                  []dcStatus `json:"dcs"`
}

type dcStatus struct {
	DC                 int      `json:"dc"`
	Endpoints          []string `json:"endpoints"`
	AvailableEndpoints int      `json:"available_endpoints"`
	AvailablePct       float64  `json:"available_pct"`
	RequiredWriters    int      `json:"required_writers"`
	FloorMin           int      `json:"floor_min"`
	FloorTarget        int      `json:"floor_target"`
	FloorMax           int      `json:"floor_max"`
	FloorCapped        bool     `json:"floor_capped"`
	AliveWriters       int      `json:"alive_writers"`
	CoveragePct        float64  `json:"coverage_pct"`
	FreshAliveWriters  int      `json:"fresh_alive_writers"`
	FreshCoveragePct   float64  `json:"fresh_coverage_pct"`
	RttMs              *float64 `json:"rtt_ms"`
	Load               int      `json:"load"`
}

func dcsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dcs",
		Short: "show DcStatusData per DC for each telemt server",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDcs(configPath, os.Stdout)
		},
	}
	return cmd
}

func runDcs(configPath string, out io.Writer) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfg.TelemtServers) == 0 {
		return fmt.Errorf("no telemt_servers in %s", configPath)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	for i, s := range cfg.TelemtServers {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "== %s ==\n", s.BaseURL)
		if s.BaseURL == "" || s.Token == "" {
			fmt.Fprintln(out, "  skipping: incomplete server entry")
			continue
		}
		data, err := fetchDcStatus(client, s.BaseURL, s.Token)
		if err != nil {
			fmt.Fprintf(out, "  error: %v\n", err)
			continue
		}
		renderDcStatus(out, data)
	}
	return nil
}

func fetchDcStatus(client *http.Client, baseURL, token string) (*dcStatusData, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/stats/dcs"
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
	var r dcStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if !r.OK {
		return nil, fmt.Errorf("non-ok response")
	}
	return &r.Data, nil
}

func renderDcStatus(out io.Writer, d *dcStatusData) {
	gen := time.Unix(d.GeneratedAtEpochSecs, 0).UTC().Format("2006-01-02 15:04:05 UTC")
	fmt.Fprintf(out, "  middle_proxy_enabled: %v\n", d.MiddleProxyEnabled)
	if d.Reason != nil && *d.Reason != "" {
		fmt.Fprintf(out, "  reason:               %s\n", *d.Reason)
	}
	fmt.Fprintf(out, "  generated_at:         %s\n", gen)
	if len(d.Dcs) == 0 {
		fmt.Fprintln(out, "  (no DCs)")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  DC\tEP\tAVAIL%\tWRITERS\tCOV%\tFRESH\tF-COV%\tFLOOR\tRTT(ms)\tLOAD")
	for _, r := range d.Dcs {
		rtt := "-"
		if r.RttMs != nil {
			rtt = fmt.Sprintf("%.1f", *r.RttMs)
		}
		floor := fmt.Sprintf("%d", r.FloorTarget)
		if r.FloorCapped {
			floor += "*"
		}
		fmt.Fprintf(tw, "  %d\t%d/%d\t%.1f\t%d/%d\t%.1f\t%d/%d\t%.1f\t%s\t%s\t%d\n",
			r.DC,
			r.AvailableEndpoints, len(r.Endpoints), r.AvailablePct,
			r.AliveWriters, r.RequiredWriters, r.CoveragePct,
			r.FreshAliveWriters, r.RequiredWriters, r.FreshCoveragePct,
			floor, rtt, r.Load,
		)
	}
	tw.Flush()
}
