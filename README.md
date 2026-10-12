# Carbonara 🍝

Open-source freight emissions calculator following the GLEC Framework / ISO 14083 approach.

Ships with the **UK DESNZ 2026** conversion factors built in: 127 freight factors (road, rail, sea, air) and 166 fuel factors, each split into tank-to-wheel and well-to-tank.

## Quick start

```sh
go install github.com/majidkabir/carbonara/cmd/carbonara@latest

carbonara calc examples/cph-rotterdam.json      # one shipment -> JSON
carbonara batch examples/shipments.csv          # many shipments -> CSV
carbonara serve -addr :8080                     # HTTP API
carbonara factors                               # list vehicle types and fuels
carbonara calc -factors my-glec.json shipment.json   # use your own dataset
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

Distance comes from `distance_km` when given, otherwise great-circle distance between `from` and `to`, adjusted per mode by the dataset's `distance_adjustments` (`gcd × multiplier + add_km`).

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

`-factors` (or `CARBONARA_FACTORS`) takes an embedded dataset name or a JSON file.

| Dataset | What |
|---|---|
| `desnz-2026` (default) | UK government GHG conversion factors 2026, imported from the official flat file |
| `sample` | Placeholder values, for tests only |

A dataset is JSON with `legs` (g CO2e/tkm), `hubs` (g CO2e/t), `fuels` (kg CO2e/unit) and optional `distance_adjustments`, `vehicle_aliases` and `fuel_aliases`, each factor with a TTW/WTT split, source and year. See [`sample.json`](pkg/factors/data/sample.json) for the shape.

**Vehicle types and fuel ids** follow the source's labels, e.g. `hgv_refrigerated_all_diesel/articulated_33t/100pct_laden` or `liquid_fuels/marine_gas_oil/tonnes`. Run `carbonara factors` to list them. Common ones have short aliases; results always show the full id used:

| Mode | Aliases |
|---|---|
| road | `hgv`, `hgv_rigid`, `hgv_artic`, `hgv_artic_33t`, `hgv_reefer`, `van`, `van_electric` |
| rail | `freight_train` |
| sea | `container_ship`, `bulk_carrier`, `general_cargo`, `roro`, `ropax` |
| air | `air_freight` (international, without radiative forcing) |
| fuel (per litre) | `diesel`, `diesel_mineral`, `petrol`, `hvo`, `marine_gas_oil`, `marine_fuel_oil`, `jet_fuel` |

Road aliases use DESNZ's "average laden" factors.

**Notes on DESNZ data:**
- Air freight comes `with_rf` and `without_rf` (radiative forcing). ISO 14083 reports without RF.
- No hub factors and no distance adjustments are included. Add your own (e.g. from GLEC) in a dataset file.
- GLEC default values can't be bundled: Smart Freight Centre limits them to members and certified tools. If you have access, put them in a dataset file.

**Updating DESNZ** each year: download the "flat file" from [gov.uk](https://www.gov.uk/government/collections/government-conversion-factors-for-company-reporting) and run

```sh
carbonara import-desnz -o pkg/factors/data/desnz-2027.json ghg-conversion-factors-2027-flat-format.xlsx
```

It reports how many rows it imported, skipped as blank, or couldn't match to a well-to-tank factor.

## Layout

| Path | What |
|---|---|
| `pkg/calc` | Domain model and calculation. Stdlib only. |
| `pkg/factors` | Factor datasets, embedded or from file. |
| `pkg/distance` | Great-circle distance with per-mode adjustment. |
| `pkg/csvio` | CSV batch input and output. |
| `internal/desnz` | DESNZ flat-file importer. |
| `internal/xlsx` | Minimal stdlib-only .xlsx reader. |
| `internal/api` | HTTP handlers and OpenAPI spec. |
| `cmd/carbonara` | CLI. |

## Roadmap

- [x] Intensity and fuel-based methods, hubs
- [x] Factor datasets (embedded + file)
- [x] Great-circle distance
- [x] CLI: `calc`, `batch`, `serve`, `factors`
- [x] HTTP API + OpenAPI
- [x] CI, GoReleaser, Docker image
- [x] Real dataset: UK DESNZ 2026, with importer for future years
- [x] Dataset-level distance adjustments
- [ ] GLEC distance adjustment factors (need a verified primary source)
- [ ] GLEC worked examples as golden tests
- [x] Short aliases for common vehicle types and fuels
- [ ] OSRM road routing, SeaRoute network for sea

## Releasing

Push a tag like `v0.1.0`: binaries go to GitHub Releases, the image to `ghcr.io/majidkabir/carbonara`.

## License

Code: Apache 2.0, see [LICENSE](LICENSE).

DESNZ data: Crown copyright, contains public sector information licensed under the [Open Government Licence v3.0](https://www.nationalarchives.gov.uk/doc/open-government-licence/version/3/). See [NOTICE](NOTICE).
