# Carbonara 🍝

Source-available freight emissions calculator following the GLEC Framework / ISO 14083 approach. Free for non-commercial use; see [License](#license).

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

### Everything, with real routing

[`compose.yaml`](compose.yaml) runs Carbonara with an OSRM road router and Eurostat's maritime network:

```sh
docker compose up -d            # first start downloads and prepares the data
open http://localhost:8080
```

| Setting | Default | |
|---|---|---|
| `OSRM_PBF_URL` | Denmark from Geofabrik | any `.osm.pbf`; preparing needs roughly 5–10× its size in RAM |
| `SEA_RESOLUTION_KM` | `20` | `5`, `10`, `20`, `50` or `100` |
| `CARBONARA_PORT` | `8080` | |
| `CARBONARA_FACTORS` | `desnz-2026,nl-iww-2026` | as `-factors` |

Prepared data is kept in volumes and reused; changing the region re-prepares it. The maritime network (EUPL-1.2) is downloaded on your machine from a pinned SeaRoute commit and checked by SHA-256. A weekly CI job brings the whole stack up on a small region and checks that road and sea legs are actually routed.

## As a Go library

```sh
go get github.com/majidkabir/carbonara
```

```go
ds, _ := factors.Open(factors.Default) // or "desnz-2026,my-tocs.json"
c := calc.Calculator{Factors: ds, Distances: ds.Distances()}
res, err := c.Calculate(ctx, calc.Shipment{
	WeightKg: 12_000,
	Legs: []calc.Leg{{Mode: calc.Road, VehicleType: "hgv", DistanceKm: 290}},
})
// res.WTWKg, res.Legs[0].DataType, res.Legs[0].Factor.Source, ...
```

`pkg/calc` has no dependencies outside the standard library; nothing in the module does. Runnable examples for each package are on [pkg.go.dev](https://pkg.go.dev/github.com/majidkabir/carbonara) (they appear after the next release is fetched there).

## Calculation

Four methods per leg:

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

**Energy** — when a leg has `energy_kwh_per_tkm` and `country` (ISO alpha-3, e.g. `DNK`), for electric trains or trucks: the electricity used per tonne-km × that country's grid intensity, all well-to-tank (nothing is emitted at the vehicle). Grid intensities for 213 countries come from Ember via Our World in Data (2024–2025 values). There's no default energy figure: give your operator's, or your own. For metered kWh use the fuel method with fuel `electricity/DNK`.

**Hubs** add `weight_t × hub g/t / 1000` (WTW) to the total. Intensity (g/tkm) covers transport only.

**Distance** comes from `distance_km` when given. Otherwise:

| Mode | Source | Distance type |
|---|---|---|
| road | an [OSRM](https://project-osrm.org) server, with `-osrm URL` | shortest feasible (`sfd`) |
| sea | shortest route on a maritime network, with `-sea-network file.geojson` | shortest feasible (`sfd`) |
| air and anything else | great-circle | great-circle (`gcd`) |

A dataset may also define `distance_adjustments` (`gcd × multiplier + add_km`) to estimate route distance from great-circle for a mode; the result then has no known type.

**Distance adjustment factors (DAF).** Following the GLEC Framework v3.2 and ISO 14083, emissions are `tonne-km × intensity × DAF`. A DAF is needed when the leg's distance type differs from the distance type a factor was built on (`distance_basis`: `sfd`, `gcd` or `actual`). Give the type of a given distance with `distance_type`. Built-in GLEC defaults:

| Mode | From → to | DAF |
|---|---|---|
| air | actual flown → great-circle | actual − 95 km (GLEC: GCD / (GCD + 95 km)) |
| sea | shortest feasible → actual | × 1.15 (Clean Cargo, cited by GLEC) |

DESNZ air factors are per great-circle tonne-km, as GLEC and ISO 14083 measure air; so a great-circle leg needs no DAF and a flown distance gets the air DAF. A mismatch with no DAF is an error rather than a silent guess. Results report `distance_type`, `daf` and `daf_source` per leg.

Each leg result records `distance_method` (`given`, `osrm`, `sea_network`, `great_circle`, `great_circle_adjusted`) so every number can be traced. Plain great-circle badly understates sea distances (Rotterdam–Shanghai: 8,970 km vs 19,696 km routed), so use a sea network for sea legs.

### Sea routing

Uses Eurostat's [SeaRoute](https://github.com/eurostat/searoute) maritime network. It's EUPL-1.2 licensed, so it isn't bundled. The Compose setup prepares it for you (or run `python deploy/prepare_sea.py 20 marnet.geojson`); with GDAL it's:

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

## Verified against GLEC

[`pkg/toc/glec_test.go`](pkg/toc/glec_test.go) reproduces the worked examples in the GLEC Framework v3.2 (Section 3, Module 4) end to end through Carbonara: fuel and activity data → TOC/HOC intensities → shipment emissions. Road (p.116), air (p.128), bulk sea with hubs (p.131–132) and a two-intensity container terminal (p.134–135) all match GLEC's published figures to the precision printed. Two notes from doing this:

- GLEC sometimes rounds an intensity before applying it (air: 1.244 kg/tkm; hub: 0.218 kg/t), so a few published results differ from the unrounded calculation in the last digit. The tests check both.
- In the bulk sea example, the final chain total (3,687,546 kg) only adds up with 1.3 kg CO₂e/t at the terminals, while the example states 1.2 kg/t; with 1.2 it matches GLEC's own sum of TCE emissions (3,679,673 kg), which is what Carbonara gives.

## Reporting

Results are shaped for ISO 14083-style reporting:

- **Per element**: every leg and hub reports its emissions separately.
- **Three components**: tank-to-wheel (`ttw_kg`), well-to-tank (`wtt_kg`) and well-to-wheel (`wtw_kg`), plus WTW intensity per tonne-km, for each leg and in total. Hub factors are WTW only.
- **Data type** per element: `primary` (measured fuel), `modelled` (load or energy model from shipment inputs) or `default` (published factor), and `primary_data_share` of total WTW.
- **Traceability**: each leg carries its factor with source and year, its `distance_method`, and the result names the `dataset`.

Carbonara isn't certified or verified against ISO 14083. Treat results as calculations for your own reporting process.

### Your own operational data (TOCs and HOCs)

Carriers can calculate from their own measured data: group similar operations into a *transport operation category* (TOC) or *hub operation category* (HOC), and total a period's fuel and electricity against the activity carried out:

```
TOC intensity (g CO2e/tkm) = Σ fuel × fuel factor / tonne-km carried (incl. empty running)
HOC intensity (g CO2e/t)   = Σ energy × energy factor / tonnes handled
```

`carbonara toc` turns an [operations file](examples/operations.json) into a dataset layer. Shipments then use the TOC id as their vehicle type, or the HOC id as their hub type, and are labelled primary data:

```sh
carbonara toc -o acme-2025.json examples/operations.json
#  TOC dk_road_ambient   83.54 g CO2e/tkm WTW (64.33 TTW + 19.22 WTT)
#  HOC cph_crossdock    777.4 g CO2e/t WTW
carbonara calc -factors desnz-2026,acme-2025.json shipment.json
```

Fuels are converted with the selected dataset's fuel factors, including `electricity/<country>`.

## Inputs

**Weight or containers.** Give `weight_kg`, or `teu` plus either `teu_net_tonnes` (net cargo tonnes per TEU) or `container_load`, a profile name from the dataset's `container_loads` (e.g. `{"light": 6, "average": 10}`). No profiles ship by default: the common light/average/heavy values come from EcoTransIT's report, which needs their permission to reuse. Results show the `weight_kg` used and its `weight_basis`. Container tare weight is not added.

**JSON** — see [`examples/cph-rotterdam.json`](examples/cph-rotterdam.json) and the [OpenAPI spec](internal/api/openapi.yaml).

**CSV** — one row per leg or hub stop, grouped by `shipment_id`. Columns (any order):

| Column | Use |
|---|---|
| `shipment_id` | required |
| `weight_kg`, or `teu` with `teu_net_tonnes` / `container_load` | cargo; can be on the first row only |
| `leg_id`, `mode`, `vehicle_type`, `distance_km`, `distance_type` | leg |
| `from_lat`, `from_lon`, `to_lat`, `to_lon` | leg, when `distance_km` is empty |
| `fuel`, `fuel_amount`, `vehicle_tonne_km` | leg, fuel method |
| `load_factor`, `empty_trip_factor` | leg, load model |
| `energy_kwh_per_tkm`, `country` | leg, energy method |
| `hub_id`, `hub_type` | hub stop (row is a hub when `hub_type` is set) |

A bad row fails only its shipment; the rest still calculate.

`batch` streams: it reads one shipment at a time and calculates in parallel (`-workers`, default one per CPU), writing results in input order. Memory stays flat — a million rows used about 50 MB. This needs each shipment's rows next to each other (sorted or grouped by `shipment_id`); split-up shipments are reported per shipment. For unsorted files use `-ungrouped`, which loads the whole file.

## Web page

`carbonara serve` also serves a page at `/`: enter a shipment leg by leg and see its emissions, a bar of each leg's share split into vehicle and fuel-supply emissions, and for every number the distance method, calculation method, data type and factor source. It opens with a worked example. Works offline (no external scripts or fonts) and in light or dark mode.

## Streaming (Kafka and pipes)

`carbonara stream` reads one shipment JSON per line and writes one result per line, in input order:

```sh
kcat -C -b broker:9092 -t shipments -u -q -o end \
  | carbonara stream \
  | kcat -P -b broker:9092 -t shipment-emissions
```

Each output line is `{"line": n, "shipment_id": "...", "result": {...}}`, or `{"line": n, "shipment_id": "...", "error": "..."}` for a message that's invalid or can't be calculated; bad messages never stop the stream. Unknown fields are rejected, so a misspelt field can't silently change a result. Output is flushed every 100 ms (`-flush`), so results flow while input is idle. On SIGINT/SIGTERM it flushes what's done and exits; results still in flight are dropped, so pair it with at-least-once consumption. Throughput: about 60,000 shipments/s on 2 cores in 15 MB of memory.

## API

| Method | Path | |
|---|---|---|
| `POST` | `/v1/calculate` | one shipment |
| `POST` | `/v1/calculate/batch` | `{"shipments": [...]}`, up to 10,000 |
| `GET` | `/v1/factors` | loaded dataset |
| `GET` | `/openapi.yaml` | spec |
| `GET` | `/healthz` | liveness |
| `GET` | `/` | web page |

Protections for running it on a network: at most `-max-concurrent` calculation requests at once (default 4 × CPUs; more get `503` with `Retry-After`), a `-request-timeout` per request (default 30 s, then `504`), `-max-legs` per shipment (default 100), 10 MiB bodies, 10,000 shipments per batch, panics answered with `500` without taking the server down, and `nosniff`/`no-store` headers. There's no authentication: put it behind your own gateway if it faces the internet.

## Factor datasets

`-factors` (or `CARBONARA_FACTORS`) takes an embedded dataset name or a JSON file, or several separated by commas to layer them: `-factors desnz-2026,my-hubs.json`. Layers are combined; defining the same factor twice is an error, so nothing is silently overridden.

| Dataset | What |
|---|---|
| `desnz-2026` | UK government GHG conversion factors 2026, imported from the official flat file |
| `nl-iww-2026` | Inland waterway factors from the Dutch [CO2 emission factor list](https://www.co2emissiefactoren.nl/) 2026 (CE Delft STREAM 2020) |

The default is `desnz-2026,nl-iww-2026`.
| `sample` | Placeholder values, for tests only |

A dataset is JSON with `legs` (g CO2e/tkm), `hubs` (g CO2e/t), `fuels` (kg CO2e/unit) and optional `load_models`, `dafs`, `distance_adjustments`, `vehicle_aliases`, `fuel_aliases` and `container_loads`, each factor with a TTW/WTT split, source and year. See [`sample.json`](pkg/factors/data/sample.json) for the shape.

**Vehicle types and fuel ids** follow the source's labels, e.g. `hgv_refrigerated_all_diesel/articulated_33t/100pct_laden` or `liquid_fuels/marine_gas_oil/tonnes`. Run `carbonara factors` to list them. Common ones have short aliases; results always show the full id used:

| Mode | Aliases |
|---|---|
| road | `hgv`, `hgv_rigid`, `hgv_artic`, `hgv_artic_33t`, `hgv_reefer`, `van`, `van_electric` |
| rail | `freight_train` |
| sea | `container_ship`, `bulk_carrier`, `general_cargo`, `roro`, `ropax` |
| air | `air_freight` (international, without radiative forcing) |
| inland_waterway | `barge` (1500–3000 t, the Dutch list's average), `container_barge` (208 TEU, likewise) |
| fuel (per litre) | `diesel`, `diesel_mineral`, `petrol`, `hvo`, `marine_gas_oil`, `marine_fuel_oil`, `jet_fuel` |

Road aliases use DESNZ's "average laden" factors.

**Inland waterways** (`nl-iww-2026`): three bulk vessel classes (`barge_bulk_small`/`medium`/`large`, 300 t to 11,000 t) and four container vessels (`barge_container_40teu`, `96teu`, `208teu`, `348teu`). Factors are per *actual* tonne-km and include sailing empty. Give the river distance as `distance_km`; a shortest feasible distance (`distance_type: sfd`) is accepted as is, since GLEC needs no DAF for inland waterways. A leg with only coordinates is refused: a straight line would undercount a river route, and there's no default correction for it. Source: www.co2emissiefactoren.nl, free to use with attribution.

**Notes on DESNZ data:**
- Air freight comes `with_rf` and `without_rf` (radiative forcing). ISO 14083 reports without RF.
- No hub factors are included, and none ship by default: the openly published averages we found (Fraunhofer IML's [logistics site values](https://reff.iml.fhg.de/), e.g. per tonne for transhipment and warehousing) carry no reuse licence. Add your own as a layer:

  ```json
  {"name": "my-hubs", "note": "where these values come from",
   "hubs": [{"hub_type": "transhipment_ambient", "wtw_g_per_tonne": 0, "source": "…", "year": 2024}]}
  ```
  Fill in real values (g CO2e per tonne handled) from a source you're allowed to use.
- Air factors are marked as great-circle based; the GLEC default DAFs above are added. Sea factors state no distance basis, so no DAF applies to them.
- GLEC default values can't be bundled: Smart Freight Centre limits them to members and certified tools. If you have access, put them in a dataset file.

**Updating DESNZ** each year: download the "flat file" from [gov.uk](https://www.gov.uk/government/collections/government-conversion-factors-for-company-reporting) and run

```sh
carbonara import-desnz -o pkg/factors/data/desnz-2027.json ghg-conversion-factors-2027-flat-format.xlsx
```

It reports how many rows it imported, skipped as blank, or couldn't match to a well-to-tank factor.

**Updating grid intensities**: `carbonara import-grid -o pkg/factors/data/grid/ember-owid.json owid-energy-data.csv`, with the CSV from [owid/energy-data](https://github.com/owid/energy-data).

## Layout

| Path | What |
|---|---|
| `pkg/calc` | Domain model and calculation. Stdlib only. |
| `pkg/factors` | Factor datasets, embedded or from file. |
| `pkg/distance` | Great-circle with per-mode adjustment, OSRM road routing, maritime network routing. |
| `pkg/csvio` | CSV input (in-memory or streaming) and output. |
| `pkg/batch` | Parallel, ordered, bounded-memory batch processing. |
| `pkg/ndjson` | Newline-delimited JSON input and output for streaming. |
| `pkg/toc` | Transport and hub operation categories from operational data. |
| `internal/desnz` | DESNZ flat-file importer. |
| `internal/xlsx` | Minimal stdlib-only .xlsx reader. |
| `internal/grid` | Grid intensity importer (Our World in Data / Ember). |
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
- [x] GLEC v3.2 worked examples as golden tests (Module 4: road, air, bulk sea, hub)
- [x] Short aliases for common vehicle types and fuels
- [x] OSRM road routing
- [x] Sea routing on a maritime network (SeaRoute)
- [x] GLEC distance types and DAFs (air: actual − 95 km → great-circle; sea: × 1.15)
- [x] Containers in TEU
- [x] Truck load model (load factor, empty trip factor)
- [x] Electricity by country (213 grids) for electric rail and trucks
- [x] Dataset layering (e.g. DESNZ + your own hub factors)
- [ ] Bundled hub / warehouse factors (needs an openly licensed source)
- [x] Web page for `carbonara serve`
- [x] Streaming, parallel batch
- [x] NDJSON stream mode for pipes and Kafka
- [x] Input validation, fuzzing of every parser in CI, API limits
- [x] Docker Compose with OSRM and the sea network
- [x] Transport and hub operation categories from your own data
- [x] Package docs and runnable examples
- [x] Bundled inland waterway factors (Dutch CO2 emission factor list)
- [ ] Inland waterway routing (a river network, like the sea one)
- [ ] Cargo-type presets (needs licensed values, e.g. EcoTransIT with permission)

## Releasing

Either:

- **From GitHub** (works on mobile): Actions → Release → Run workflow, enter a version like `v0.1.0`. It runs the tests, creates the tag, then builds.
- **From a terminal**: `git tag v0.1.0 && git push origin v0.1.0`.

Binaries for Linux, macOS and Windows go to GitHub Releases, the image to `ghcr.io/majidkabir/carbonara`.

## License

Code: [PolyForm Noncommercial 1.0.0](LICENSE). Free for personal, educational, research, non-profit and government use; commercial use needs a separate licence from the author. This matches the terms of the GLEC Framework, which Carbonara follows. Versions up to and including v0.1.0 were released under Apache 2.0 and stay available under it.

Required Notice: Copyright 2026 majidkabir (https://github.com/majidkabir/carbonara)

DESNZ data: Crown copyright, contains public sector information licensed under the [Open Government Licence v3.0](https://www.nationalarchives.gov.uk/doc/open-government-licence/version/3/). Grid intensities: Ember via Our World in Data, [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). See [NOTICE](NOTICE).
