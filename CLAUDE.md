# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

`tctl` — a Go CLI (cobra) that pulls unique user IPs from telemt servers and aggregates them by country/city using MaxMind GeoLite2. See `README.md` for the user-facing description.

## Command structure

Two commands, with `aggregate` nested under `collect`:

- `tctl collect` (alias `c`) — fetches `/v1/users` from each `telemt_servers` entry in `.tctl.yaml`, merges `recent_unique_ips_list` values with what's already in `collected_ips.yaml` (deduplicated), rewrites the file.
- `tctl collect aggregate` (alias `a`) — reads `collected_ips.yaml`, looks up each IP in `mmdb/GeoLite2-City.mmdb` via `maxminddb-golang` (no shelling out), groups by `(country, city)`, writes `aggregated_geo.json` sorted by `count` descending. `--max-age 45s|4m|24h|2d` deletes the input after a successful aggregation when its `created_at` is older than the given duration (so the next `collect` run starts a fresh accumulator).

Aliases can be chained: `tctl c a`.

## Layout

- `main.go` — root cobra command, registers `collect`.
- `collect.go` — `collect` command + adds `aggregate` as its subcommand. Owns shared types `server`, `config`, `ipList`.
- `aggregate.go` — `aggregate` command, mmdb lookup + grouping.
- `.tctl.yaml` — config (default; override with `-c/--conf`). Contains `telemt_servers` with `base_url` and bearer `token`.
- `collected_ips.yaml` — accumulated unique IPs (output of `collect`, input of `aggregate`). Persistent across runs. Carries `created_at` (preserved across `collect` runs; set on the first write) and `last_update` (rewritten every `collect`).
- `aggregated_geo.json` — final aggregate.
- `mmdb/GeoLite2-{ASN,City,Country}.mmdb` — MaxMind databases. Large binary files; never `Read` them with the file tool.
- `update-mmdb` — bash script that curls the three latest `.mmdb` files from `P3TERX/GeoLite.mmdb`. It writes to the working directory, so run it from `mmdb/`: `cd mmdb && ../update-mmdb`.
- `Makefile` — `make build` (host) / `make linux` (linux-amd64) into `.bin/`.
- `README_.md` — legacy reference notes with raw `mmdbinspect`/`jq` recipes. Not the user-facing README.

## Working in this repo

- Build with `make build` after Go changes; the resulting `.bin/tctl` is what to invoke for end-to-end checks.
- The default mmdb path in `aggregate` is `mmdb/GeoLite2-City.mmdb`. `update-mmdb` doesn't `cd` itself, so files end up where it's run.
- `collected_ips.yaml` is an accumulator, not a temp file — don't blow it away to "reset state" without asking.
- telemt-side prerequisite: `[access].user_max_unique_ips_mode = "time_window"` with `user_max_unique_ips_window_secs = 120`. Without it, `/v1/users` returns stale or empty IP lists. Documented in `README.md`.
