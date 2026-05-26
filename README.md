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
collect_file_path: /var/lib/tctl/%Y/%m/%d/%H.yaml
aggregate_file_path: /var/lib/tctl/%Y/%m/%d/%H.json
days_file_path: /var/lib/tctl/days.json
mmdb_city: /var/lib/tctl/mmdb/GeoLite2-City.mmdb
mmdb_asn: /var/lib/tctl/mmdb/GeoLite2-ASN.mmdb
mmdb_country: /var/lib/tctl/mmdb/GeoLite2-Country.mmdb
telemt_servers:
  - base_url: https://s1.example.com:9091
    token: <bearer-token>
    tags: [nl, primary]
  - base_url: https://s2.example.com:9091
    token: <bearer-token>
    tags: [de]
```

`tags` is optional. It's used by `tctl dcs -t <tag>` to filter which servers to query.

| Field                  | Used by              | Notes |
| ---------------------- | -------------------- | ----- |
| `collect_file_path`    | `collect`, `aggregate` | output of `collect`, input of `aggregate` |
| `aggregate_file_path`  | `aggregate`            | output of `aggregate` |
| `days_file_path`       | `collect`              | global index of collected days; defaults to `/var/lib/tctl/days.json` when unset |
| `mmdb_city`            | `aggregate`            | required for city/country lookups |
| `mmdb_asn`, `mmdb_country` | reserved          | declared for future commands |
| `telemt_servers`       | `collect`              | list of `{base_url, token}` |

`collect_file_path`, `aggregate_file_path`, and `days_file_path` support date placeholders expanded against the current UTC time on each run:

| Placeholder | Meaning              |
| ----------- | -------------------- |
| `%Y`        | 4-digit year         |
| `%m`        | 2-digit month        |
| `%d`        | 2-digit day          |
| `%H`        | 2-digit hour (24h)   |
| `%M`        | 2-digit minute       |
| `%S`        | 2-digit second       |
| `%%`        | literal `%`          |

Missing parent directories are created automatically. With the example above and `%H.yaml`, you get a separate accumulator file per hour.

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

Hits `GET /v1/users` with `Authorization: Bearer <token>` on each server in `telemt_servers`, merges all `recent_unique_ips_list` values with the IPs already stored at the resolved `collect_file_path` (deduplicated), and rewrites the file with an updated timestamp.

```sh
tctl collect
```

The output path comes from `collect_file_path` in the config (no `-o` flag); see [Configuration](#configuration) for placeholder semantics.

After the YAML is written, `collect` also updates a global day index at `days_file_path` (default `/var/lib/tctl/days.json`): the current UTC date in `YYYY-MM-DD` is added to a JSON array, deduplicated and sorted ascending. Useful as an index of which days have collected data on disk.

```json
[
  "2026-05-24",
  "2026-05-25"
]
```

### `tctl collect aggregate` (alias `a`)

Reads the file at `collect_file_path` (with current-UTC placeholders expanded), looks up each IP in `mmdb_city`, groups by `(country, city)`, and writes the result to `aggregate_file_path`. Parent directories are created automatically.

```sh
tctl collect aggregate
tctl c a                # chained aliases
```

All paths come from the config; there are no input/output/db flags. If `collect` and `aggregate` run in the same hour (with the example `%H` granularity), they line up on the same time-stamped pair.

The aggregate JSON is an object with summary counts and a `data` array of per-(country, city) groups sorted by `count` descending:

```json
{
  "country_count": 42,
  "city_count": 137,
  "data": [
    {
      "country": "Russia",
      "city": "Moscow",
      "latitude": 55.7487,
      "longitude": 37.6187,
      "count": 35
    }
  ]
}
```

`country_count` is the number of distinct countries in `data`; `city_count` is the number of distinct non-null cities. `country` and `city` may be `null` when the MaxMind record lacks the corresponding field.

### `tctl dcs`

Iterates every `telemt_servers` entry, calls `GET /v1/stats/dcs` (`DcStatusData`), and prints a per-server table with the most operationally useful fields:

```
== https://s1.example.com:9091 ==
  middle_proxy_enabled: true
  generated_at:         2026-05-15 14:30:00 UTC
  DC  EP   AVAIL%  WRITERS  COV%   FRESH   F-COV%  FLOOR  RTT(ms)  LOAD
  1   3/3  100.0   12/12    100.0  11/12   91.7    12     45.2     128
  2   1/2  50.0    6/8      75.0   5/8     62.5    10*    -        42

== https://s2.example.com:9091 ==
  ...
```

Columns: `EP` is `available/total` endpoints, `WRITERS` is `alive/required`, `FRESH` is `fresh_alive/required`, `FLOOR` is `floor_target` (`*` suffix when `floor_capped`), `RTT(ms)` shows `-` when null, `LOAD` is bound client sessions. Per-server errors are inlined under that server's header; iteration continues over the remaining servers.

`-t/--tag` filters which servers to hit by their `tags`. Repeatable; matches if any of the server's tags intersects with any of the filter values. Without `-t`, all servers are queried.

```sh
tctl dcs
tctl dcs -t nl
tctl dcs -t nl -t nl2     # union: servers tagged nl OR nl2
```

## MaxMind databases

`tctl-update-mmdb` downloads the latest `GeoLite2-{ASN,City,Country}.mmdb` from the [P3TERX/GeoLite.mmdb](https://github.com/P3TERX/GeoLite.mmdb) releases. Place them where `mmdb_city` / `mmdb_asn` / `mmdb_country` point.

```sh
./tctl-update-mmdb
```

## Files

| File | Purpose |
| --- | --- |
| `.tctl.yaml` | config (paths + telemt servers) |
| `collect_file_path` target | accumulated unique IPs (output of `collect`, input of `aggregate`); carries `count`, `created_at` and `last_update` metadata |
| `aggregate_file_path` target | aggregated country/city summary (`{country_count, city_count, data:[...]}`) |
| `days_file_path` target | sorted JSON array of `YYYY-MM-DD` dates on which `collect` has run |
| `mmdb_*` targets | MaxMind databases |
