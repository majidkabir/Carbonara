# Carbonara 🍝

Open-source freight emissions calculator following the GLEC Framework / ISO 14083 approach.

> ⚠️ The embedded `sample` factors are **placeholders**. Load real GLEC or DEFRA factors with `-factors` (or `CARBONARA_FACTORS`) before using results.

## Quick start

```sh
go install github.com/majidkabir/carbonara/cmd/carbonara@latest

carbonara calc examples/cph-rotterdam.json      # one shipment -> JSON
carbonara batch examples/shipments.csv          # many shipments -> CSV
carbonara serve -addr :8080                     # HTTP API
carbonara factors -factors my-factors.json      # inspect a dataset
```

Docker:

```sh
docker run -p 8080:8080 ghcr.io/majidkabir/carbonara
curl -X POST localhost:8080/v1/calculate --data @examples/cph-rotterdam.json
```

## Calculation

Two methods per leg, as in ISO 14083:

**Intensity (modelled data)** — the default:
```
tonne-km = weight_t × distance_km
TTW kg   = tonne-km × TTW g/tkm / 1000
WTW kg   = tonne-km × (TTW + WTT) g/tkm / 1000
```

**Fuel (primary data)** — when a leg has `fuel`:
```
share  = shipment tonne-km / vehicle_tonne_km   (1 if not given)
TTW kg = fuel_amount × TTW kg/unit × share
WTW kg = fuel_amount × (TTW + WTT) kg/unit × share
```

**Hubs** add `weight_t × hub g/t / 1000` (WTW) to the total. Intensity (g/tkm) covers transport only.

Distance comes from `distance_km` when given, otherwise great-circle distance between `from` and `to`.

## Inputs

**JSON** — see [`examples/cph-rotterdam.json`](examples/cph-rotterdam.json) and the [OpenAPI spec](internal/api/openapi.yaml).

**CSV** — one row per leg or hub stop, grouped by `shipment_id`. Columns (any order):

| Column | Use |
|---|---|
| `shipment_id`, `weight_kg` | required; weight can be on the first row only |
| `leg_id`, `mode`, `vehicle_type`, `distance_km` | leg |
| `from_lat`, `from_lon`, `to_lat`, `to_lon` | leg, when `distance_km` is empty |
| `fuel`, `fuel_amount`, `vehicle_tonne_km` | leg, fuel method |
| `hub_id`, `hub_type` | hub stop (row is a hub when `hub_type` is set) |

A bad row fails only its shipment; the rest still calculate.

## API

| Method | Path | |
|---|---|---|
| `POST` | `/v1/calculate` | one shipment |
| `POST` | `/v1/calculate/batch` | `{"shipments": [...]}`, up to 10,000 |
| `GET` | `/v1/factors` | loaded dataset |
| `GET` | `/openapi.yaml` | spec |
| `GET` | `/healthz` | liveness |

## Factor datasets

JSON with `legs` (g CO2e/tkm), `hubs` (g CO2e/t) and `fuels` (kg CO2e/unit), each with TTW/WTT split, source and year. See [`pkg/factors/data/sample.json`](pkg/factors/data/sample.json) for the shape.

## Layout

| Path | What |
|---|---|
| `pkg/calc` | Domain model and calculation. Stdlib only. |
| `pkg/factors` | Factor datasets, embedded or from file. |
| `pkg/distance` | Great-circle distance with per-mode adjustment. |
| `pkg/csvio` | CSV batch input and output. |
| `internal/api` | HTTP handlers and OpenAPI spec. |
| `cmd/carbonara` | CLI. |

## Roadmap

- [x] Intensity and fuel-based methods, hubs
- [x] Factor datasets (embedded + file)
- [x] Great-circle distance
- [x] CLI: `calc`, `batch`, `serve`, `factors`
- [x] HTTP API + OpenAPI
- [x] CI, GoReleaser, Docker image
- [ ] Real GLEC/DEFRA datasets + GLEC distance adjustment factors
- [ ] GLEC worked examples as golden tests
- [ ] OSRM road routing, SeaRoute network for sea

## Releasing

Push a tag like `v0.1.0`: binaries go to GitHub Releases, the image to `ghcr.io/majidkabir/carbonara`.

## License

Apache 2.0 — see [LICENSE](LICENSE).
