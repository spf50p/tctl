# tctl

CLI for collecting unique user IPs from telemt servers and aggregating them by country/city using MaxMind GeoLite2.

## Build

Requires Go 1.26+.

```sh
make build   # .bin/tctl (host OS)
make linux   # .bin/tctl-linux-amd64
```

## Workflow

```sh
# 1. one-time: pull MaxMind databases into mmdb/
./tctl-update-mmdb

# 2. collect unique IPs from telemt servers (repeat on cadence)
tctl c

# 3. regenerate the country/city aggregate
tctl c a
```

`collect` is incremental — each run merges new IPs into `collected_ips.yaml`, so running it repeatedly grows the dataset. `aggregate` is idempotent; rerun it whenever you want a fresh `aggregated_geo.json`.

## Configuration

Reads `.tctl.yaml` by default (override with `-c/--conf`):

```yaml
telemt_servers:
  - base_url: https://s1.example.com:9091
    token: <bearer-token>
  - base_url: https://s2.example.com:9091
    token: <bearer-token>
```

### telemt-side config

For `/v1/users` to return a meaningful `recent_unique_ips_list`, `telemt` should be configured to track IPs in a sliding window:

```toml
[access]
user_max_unique_ips_mode = "time_window"
user_max_unique_ips_window_secs = 120
```

Without this, the IP list per user is either never rotated or empty, depending on the default mode.

The window must cover your `collect` cadence with some margin: at one run per minute, 120 seconds is enough. If you collect less often, raise `user_max_unique_ips_window_secs` accordingly — IPs that age out between runs are lost.

## Commands

### `tctl collect` (alias `c`)

Hits `GET /v1/users` with `Authorization: Bearer <token>` on each server in `telemt_servers`, merges all `recent_unique_ips_list` values with the IPs already stored in `collected_ips.yaml` (deduplicated), and rewrites the file with an updated timestamp.

```sh
tctl collect            # writes collected_ips.yaml
tctl collect -o ips.yaml
```

### `tctl collect aggregate` (alias `a`)

Reads `collected_ips.yaml`, looks up each IP in `mmdb/GeoLite2-City.mmdb`, groups by `(country, city)`, and writes `aggregated_geo.json` sorted by `count` descending.

```sh
tctl collect aggregate
tctl c a                # chained aliases
tctl c a -i ips.yaml -o geo.json --db mmdb/GeoLite2-City.mmdb
tctl c a --max-age 24h  # delete -i file after aggregating if older than 24h
```

`--max-age` accepts `45s`/`4m`/`24h`/`2d` (or any `time.ParseDuration` form). The age is measured against the `created_at` field inside the input file; if the file is older than the given duration, it gets removed after a successful aggregation. The next `collect` run starts a fresh accumulator. If the input has no `created_at` (e.g. file produced before this field existed), the check is skipped with a warning.

Sample entry in `aggregated_geo.json`:

```json
{
  "country": "Russia",
  "city": "Moscow",
  "latitude": 55.7487,
  "longitude": 37.6187,
  "count": 35
}
```

`country` and `city` may be `null` when the MaxMind record lacks the corresponding field.

## MaxMind databases

`tctl-update-mmdb` downloads the latest `GeoLite2-{ASN,City,Country}.mmdb` from the [P3TERX/GeoLite.mmdb](https://github.com/P3TERX/GeoLite.mmdb) releases. Files should live under `mmdb/` (the default path `aggregate` looks at):

```sh
./tctl-update-mmdb
```

## Files

| File | Purpose |
| --- | --- |
| `.tctl.yaml` | telemt server list (config) |
| `collected_ips.yaml` | accumulated unique IPs (input for `aggregate`); carries `count`, `created_at` and `last_update` metadata |
| `aggregated_geo.json` | aggregated country/city summary |
| `mmdb/GeoLite2-*.mmdb` | MaxMind databases |
