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
| `leg_id`, `mode`, `vehicle_type`, `distance_km` | leg |
| `from_lat`, `from_lon`, `to_lat`, `to_lon` | leg, when `distance_km` is empty |
| `fuel`, `fuel_amount`, `vehicle_tonne_km` | leg, fuel method |
| `load_factor`, `empty_trip_factor` | leg, load model |
| `energy_kwh_per_tkm`, `country` | leg, energy method |
| `hub_id`, `hub_type` | hub stop (row is a hub when `hub_type` is set) |

A bad row fails only its shipment; the rest still calculate.

`batch` streams: it reads one shipment at a time and calculates in parallel (`-workers`, default one per CPU), writing results in input order. Memory stays flat — a million rows used about 50 MB. This needs each shipment's rows next to each other (sorted or grouped by `shipment_id`); split-up shipments are reported per shipment. For unsorted files use `-ungrouped`, which loads the whole file.

## Web page

`carbonara serve` also serves a page at `/`: enter a shipment leg by leg and see its emissions, a bar of each leg's share split into vehicle and fuel-supply emissions, and for every number the distance method, calculation method, data type and factor source. It opens with a worked example. Works offline (no external scripts or fonts) and in light or dark mode.

## API

| Method | Path | |
|---|---|---|
| `POST` | `/v1/calculate` | one shipment |
| `POST` | `/v1/calculate/batch` | `{"shipments": [...]}`, up to 10,000 |
| `GET` | `/v1/factors` | loaded dataset |
| `GET` | `/openapi.yaml` | spec |
| `GET` | `/healthz` | liveness |
| `GET` | `/` | web page |

## Factor datasets

`-factors` (or `CARBONARA_FACTORS`) takes an embedded dataset name or a JSON file, or several separated by commas to layer them: `-factors desnz-2026,my-hubs.json`. Layers are combined; defining the same factor twice is an error, so nothing is silently overridden.

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
- No hub factors are included, and none ship by default: the openly published averages we found (Fraunhofer IML's [logistics site values](https://reff.iml.fhg.de/), e.g. per tonne for transhipment and warehousing) carry no reuse licence. Add your own as a layer:

  ```json
  {"name": "my-hubs", "note": "where these values come from",
   "hubs": [{"hub_type": "transhipment_ambient", "wtw_g_per_tonne": 0, "source": "…", "year": 2024}]}
  ```
  Fill in real values (g CO2e per tonne handled) from a source you're allowed to use.
- The only distance adjustment is the air rule above (a convention, not DESNZ data).
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
- [ ] GLEC distance adjustment factors (need a verified primary source)
- [ ] GLEC worked examples as golden tests
- [x] Short aliases for common vehicle types and fuels
- [x] OSRM road routing
- [x] Sea routing on a maritime network (SeaRoute), air = great-circle + 95 km
- [x] Containers in TEU
- [x] Truck load model (load factor, empty trip factor)
- [x] Electricity by country (213 grids) for electric rail and trucks
- [x] Dataset layering (e.g. DESNZ + your own hub factors)
- [ ] Bundled hub / warehouse factors (needs an openly licensed source)
- [x] Web page for `carbonara serve`
- [x] Streaming, parallel batch
- [x] Transport and hub operation categories from your own data
- [x] Package docs and runnable examples
- [ ] Cargo-type presets (needs licensed values, e.g. EcoTransIT with permission)

## Releasing

Either:

- **From GitHub** (works on mobile): Actions → Release → Run workflow, enter a version like `v0.1.0`. It runs the tests, creates the tag, then builds.
- **From a terminal**: `git tag v0.1.0 && git push origin v0.1.0`.

Binaries for Linux, macOS and Windows go to GitHub Releases, the image to `ghcr.io/majidkabir/carbonara`.

## License

Code: Apache 2.0, see [LICENSE](LICENSE).

DESNZ data: Crown copyright, contains public sector information licensed under the [Open Government Licence v3.0](https://www.nationalarchives.gov.uk/doc/open-government-licence/version/3/). Grid intensities: Ember via Our World in Data, [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). See [NOTICE](NOTICE).
