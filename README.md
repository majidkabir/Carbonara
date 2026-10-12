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

Three methods per leg:

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

**Load model** — when a leg has `load_factor` (and optionally `empty_trip_factor`), for truck classes:
```
E(load)  = emissions per vehicle-km at that load (linear between 0%, 50%, 100% laden)
g/tkm    = (E(load) + empty_trip_factor × E(empty)) / (payload_t × load_factor)
```
This is the capacity-utilisation approach EcoTransIT World and GLEC use, built entirely from DESNZ data: per-km factors at 0/50/100% laden give the load curve, and per-km ÷ per-tonne-km at 100% laden gives each class's payload (e.g. 17.9 t for an artic over 33 t). It reproduces DESNZ's per-tonne-km factors at 50% and 100% load within 0.82% WTW. (DESNZ's own well-to-tank rows per km and per tonne-km disagree by up to 3.6%; the model follows the per-km rows.) Works with aliases like `hgv`, or class ids like `hgv_non_refrigerated_all_diesel/articulated_33t`. Vans have no load data in DESNZ, so no model. Cargo-type presets (bulk/average/volume load and empty-trip values) aren't included: EcoTransIT's values need their permission to reuse.

**Hubs** add `weight_t × hub g/t / 1000` (WTW) to the total. Intensity (g/tkm) covers transport only.

**Distance** comes from `distance_km` when given. Otherwise, following the same approach as EcoTransIT World:

| Mode | Source |
|---|---|
| road | an [OSRM](https://project-osrm.org) server, with `-osrm URL` |
| sea | shortest route on a maritime network, with `-sea-network file.geojson` |
| air | great-circle + 95 km (EN 16258 convention) |
| anything else | great-circle, adjusted by the dataset's `distance_adjustments` (`gcd × multiplier + add_km`) |

Each leg result records `distance_method` (`given`, `osrm`, `sea_network`, `great_circle`, `great_circle_adjusted`) so every number can be traced. Plain great-circle badly understates sea distances (Rotterdam–Shanghai: 8,970 km vs 19,696 km routed), so use a sea network for sea legs.

### Sea routing

Uses Eurostat's [SeaRoute](https://github.com/eurostat/searoute) maritime network. It's EUPL-1.2 licensed, so it isn't bundled; convert it once with GDAL:

```sh
curl -LO https://github.com/eurostat/searoute/raw/master/modules/core/src/main/resources/marnet/marnet_plus_20km.gpkg
ogr2ogr -f GeoJSON marnet_20km.geojson marnet_plus_20km.gpkg
carbonara calc -sea-network marnet_20km.geojson shipment.json
```

Straits and canals can be closed with `-sea-avoid suez,panama`. The Arctic routes (`northwest`, `northeast`) are closed by default; open them with `-sea-open`. Points are snapped to the nearest network node; `-sea-max-snap-km` rejects points too far from the sea.

On the 20 km network, distances are within 0.3% of SeaRoute's own output for 7 of 8 test routes (Rotterdam–Shanghai, Hamburg–New York, Yokohama–Los Angeles, …), and 1.6% for a short Baltic route where endpoint snapping dominates.

### Road routing

Run your own OSRM server; the public demo server isn't meant for production traffic:

```sh
docker run -t -v "$PWD:/data" ghcr.io/project-osrm/osrm-backend osrm-extract -p /opt/car.lua /data/denmark-latest.osm.pbf
docker run -t -v "$PWD:/data" ghcr.io/project-osrm/osrm-backend osrm-partition /data/denmark-latest.osrm
docker run -t -v "$PWD:/data" ghcr.io/project-osrm/osrm-backend osrm-customize /data/denmark-latest.osrm
docker run -t -p 5000:5000 -v "$PWD:/data" ghcr.io/project-osrm/osrm-backend osrm-routed --algorithm mld /data/denmark-latest.osrm
carbonara calc -osrm http://localhost:5000 examples/cph-rotterdam.json
```

## Inputs

**Weight or containers.** Give `weight_kg`, or `teu` plus either `teu_net_tonnes` (net cargo tonnes per TEU) or `container_load`, a profile name from the dataset's `container_loads` (e.g. `{"light": 6, "average": 10}`). No profiles ship by default: the common light/average/heavy values come from EcoTransIT's report, which needs their permission to reuse. Results show the `weight_kg` used and its `weight_basis`. Container tare weight is not added.

**JSON** — see [`examples/cph-rotterdam.json`](examples/cph-rotterdam.json) and the [OpenAPI spec](internal/api/openapi.yaml).

**CSV** — one row per leg or hub stop, grouped by `shipment_id`. Columns (any order):

| Column | Use |
|---|---|
| `shipment_id` | required |
| `weight_kg`, or `teu` with `teu_net_tonnes` / `container_load` | cargo; can be on the first row only |
| `leg_id`, `mode`, `vehicle_type`, `distance_km` | leg |
| `from_lat`, `from_lon`, `to_lat`, `to_lon` | leg, when `distance_km` is empty |
| `fuel`, `fuel_amount`, `vehicle_tonne_km` | leg, fuel method |
| `load_factor`, `empty_trip_factor` | leg, load model |
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

A dataset is JSON with `legs` (g CO2e/tkm), `hubs` (g CO2e/t), `fuels` (kg CO2e/unit) and optional `load_models`, `distance_adjustments`, `vehicle_aliases`, `fuel_aliases` and `container_loads`, each factor with a TTW/WTT split, source and year. See [`sample.json`](pkg/factors/data/sample.json) for the shape.

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
- No hub factors are included. The only distance adjustment is the air rule above (a convention, not DESNZ data). Add your own (e.g. from GLEC) in a dataset file.
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
| `pkg/distance` | Great-circle with per-mode adjustment, OSRM road routing, maritime network routing. |
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
- [x] OSRM road routing
- [x] Sea routing on a maritime network (SeaRoute), air = great-circle + 95 km

## Releasing

Push a tag like `v0.1.0`: binaries go to GitHub Releases, the image to `ghcr.io/majidkabir/carbonara`.

## License

Code: Apache 2.0, see [LICENSE](LICENSE).

DESNZ data: Crown copyright, contains public sector information licensed under the [Open Government Licence v3.0](https://www.nationalarchives.gov.uk/doc/open-government-licence/version/3/). See [NOTICE](NOTICE).
