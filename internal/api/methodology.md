# How Carbonara calculates freight emissions

Carbonara calculates the greenhouse gas emissions of freight transport following the approach of the
GLEC Framework v3.2 (Smart Freight Centre, 2025) and ISO 14083:2023. It is source-available
(PolyForm Noncommercial) at https://github.com/majidkabir/carbonara. It is not certified or verified
against ISO 14083: its results are calculations for the user's own reporting process.

## Scope

- **Gases:** CO2 equivalents (CO2e), in kg, as the emission factors give them (CO2, CH4, N2O).
- **Well-to-wheel (WTW)** = **tank-to-wheel (TTW)**, the vehicle's own operational emissions, +
  **well-to-tank (WTT)**, the emissions of producing and delivering its fuel or electricity
  (ISO 14083 5.2; GLEC p.10, p.57). Every result gives all three.
- **Air** is calculated without radiative forcing (non-CO2 effects at altitude), as ISO 14083 reports.
  DESNZ's factors with radiative forcing are available as vehicle types ending in `with_rf`.
- A **shipment** is a transport chain (ISO 14083 3.1.25). Each **leg** is a transport chain element
  (one mode, one vehicle type); **hubs** are transhipment or storage stops.

## Inputs

- **Cargo:** `weight_kg`; or `teu` with `teu_net_tonnes` (net cargo tonnes per TEU) or a
  `container_load` profile from the dataset. No container tare weight is added.
- **Leg:** `mode` (`road`, `rail`, `sea`, `air`, `inland_waterway`), `vehicle_type` (a short alias such as
  `hgv` or a full dataset id; see list_vehicle_types), and where it goes: `from` and `to` as
  `{"lat", "lon"}` (optionally `"name"`) or `{"code": "NLRTM"}` (UN/LOCODE) / `{"code": "CPH"}` (IATA);
  or `distance_km` with `distance_type` (`sfd`, `gcd` or `actual`).
- **Optional per leg:** measured `fuel`; a truck's load (`load_tonnes`, or `load_factor`) and
  `empty_trip_factor`; electricity use (`energy_kwh_per_tkm` with `country`). See Methods.
- **Hubs:** `{"id", "hub_type"}`. No hub factors ship by default (the openly published averages carry
  no reuse licence), so hubs need a dataset that defines them.

## Methods (one per leg)

1. **Intensity** (default value, ISO 14083 7.2.5): `tonne-km = tonnes × distance`;
   `emissions = tonne-km × intensity (g CO2e/tkm) × DAF ÷ 1000`. The intensity is a published
   average for the vehicle type.
2. **Load model** (modelled, ISO 14083 7.2.4), for trucks when `load_tonnes` or `load_factor` is given:
   emissions per vehicle-km at that load, interpolated between DESNZ's 0 %, 50 % and 100 % laden
   values, plus `empty_trip_factor` × the empty vehicle's emissions, divided by the tonnes on board:
   `g/tkm = (E(load) + empty_trip_factor × E(empty)) / (payload_t × load_factor)`.
   **Important:** `load_factor` is a share of the model's `payload_t`, DESNZ's *average full load*
   (17.9 t for an articulated truck over 33 t), **not** the truck's rated payload as in GLEC or
   EcoTransIT (about 26 t for a 40 t truck). So `load_factor: 0.6` means 10.7 t here, but about 16 t
   in GLEC's terms. When the weight on the truck is known, give `load_tonnes` instead: it means the
   same in every tool. Results say how many tonnes a load factor meant. Loads above `payload_t` are
   allowed up to 1.6 × it. Vans have no load model.
3. **Fuel** (primary data, ISO 14083 7.2.3), when the leg has measured `fuel` (`fuel`, `amount` in the
   fuel's unit, optional `vehicle_tonne_km` of the whole trip): `emissions = amount × fuel factor ×
   share`, `share = shipment tonne-km ÷ vehicle_tonne_km` (1 when not given).
4. **Energy** (modelled), for electric trains or trucks, with `energy_kwh_per_tkm` and `country`
   (ISO alpha-3): kWh per tonne-km × that country's grid intensity, all well-to-tank. There is no default
   kWh figure: the user must give the operator's.

**Hubs** add `tonnes × hub g CO2e/t ÷ 1000` (WTW only). Intensity per tonne-km covers transport only.

## Distances

A leg's distance is `distance_km` when given. Otherwise it is routed from the coordinates:

| Mode | Route | Distance type |
|---|---|---|
| road | OSRM on OpenStreetMap roads (worldwide on the public service, in overlapping regions joined where they overlap) | shortest feasible (SFD) |
| rail | OSRM with a freight-train profile on OpenStreetMap railways | SFD |
| inland waterway | OSRM with a barge profile on navigable OpenStreetMap waterways (CEMT class I+) | SFD |
| sea | shortest path on Eurostat's SeaRoute maritime network | SFD |
| air | great circle | great-circle (GCD) |

Without a router for a mode, Carbonara falls back to the great circle; for container ships and inland
waterways, whose factors are per actual distance, a leg given only by coordinates is then refused
instead (give `distance_km`). The section "This server" at the end says which modes are routed here. Each leg reports its
`distance_method` (`given`, `osrm`, `sea_network`, `great_circle`, …) and `distance_type`.

- **Ferries on road routes:** kilometres a road route crosses by ferry are calculated as a sea part
  (`parts`), with DESNZ's RoPax ferry factor, the rest with the truck.
- **Electrified rail:** rail routes record electrified track (`route_parts_km`). With
  `energy_kwh_per_tkm`, track tagged as not electrified uses the vehicle type's own (diesel) factor.

### Distance adjustment factors (DAF)

Emissions are `tonne-km × intensity × DAF` (GLEC p.22). A DAF applies when the leg's distance type
differs from the type the factor was built on (`distance_basis`):

| Mode | From → to | DAF | Source |
|---|---|---|---|
| sea | SFD → actual | × 1.15 | GLEC p.51, citing Clean Cargo |
| air | actual flown → GCD | − 95 km | GLEC p.38 |
| inland waterway | SFD → actual | × 1 | GLEC: the network leaves little room for deviation |

A mismatch without a DAF is an error, not a silent guess. Each leg reports `daf` and `daf_source`.

### Missing road legs (on by default)

- **Gaps:** when a leg starts more than 5 km from where the previous one ended, a road leg is added.
- **First and last mile:** a sea leg that starts or ends inland (more than 50 km from the sea network
  and not at a sea port) is moved to the large cargo sea port giving the best trip, and a road leg is
  added; likewise rail (5 km) and river (10 km) legs off their network go to a terminal or port.
- Added legs are marked `added` (`gap` or `access`) with a `note`; moved legs carry a `note`. Turn this
  off with `add_road_legs: false`; gaps then appear in `warnings` instead.

## Emission factors

The default dataset is `desnz-2026,nl-iww-2026,eu-mrv-2024`:

- **UK DESNZ 2026** GHG conversion factors (Open Government Licence v3.0): road, rail, sea, air and
  fuels, each split into TTW and WTT. Road aliases use DESNZ's "average laden" values.
- **Inland waterways:** the Dutch CO2 emission factor list 2026 (CE Delft STREAM 2020), per actual
  tonne-km, including sailing empty.
- **Container ships:** from EU MRV 2024 data published by EMSA (fuel, CO2e and transport work that
  about 2,000 container ships calling at EU ports reported), split into short-sea (at least half their
  voyage CO2 between EU ports) and deep-sea fleets, per actual tonne-km. The alias `container_ship`
  means the short-sea factor on legs under 2,000 km and the deep-sea factor from 2,000 km (Carbonara's
  threshold, not a standard's); the leg's `factor_note` says which.
  `container_ship_short_sea` / `container_ship_deep_sea` choose one; DESNZ's container ships by size
  stay available by their full ids.
- **Electricity grids:** Ember via Our World in Data (CC BY 4.0), 213 countries.

Known limitations:

- GLEC's own default values are not included (Smart Freight Centre restricts them), so results can
  differ from tools that use them.
- DESNZ's RoPax ferry factor is high: about twice what GLEC gives for a Mediterranean RoPax crossing.
- The EU MRV container factors come from ships calling at EU ports; on lanes that never touch Europe
  they are an approximation.

## Data quality

Each leg and hub has a `data_type` (ISO 14083 3.3.3): `primary` (measured fuel, or the operator's own
intensities), `modelled` (load or energy model) or `default` (published average). The result's
`primary_data_share` is the share of WTW from primary data.

## Reading a result

- Totals: `wtw_kg`, `ttw_kg`, `wtt_kg`, `tonne_km`, `intensity_g_per_tkm` (WTW per tonne-km,
  transport only), `primary_data_share`, `dataset`, `warnings`.
- Per leg (`legs[]`): `distance_km`, `distance_method`, `distance_type`, `daf`, `daf_source`,
  `tonne_km`, `ttw_kg`, `wtt_kg`, `wtw_kg`, `method` (`intensity`, `load_model`, `fuel`, `energy`),
  `data_type`, `factor` (the intensity used, with `source`, `year` and `distance_basis`),
  `factor_note`, `added`, `note`, load-model fields (`load_factor`, `load_tonnes`, `payload_t`,
  `empty_trip_factor`), `route_parts_km` and `parts`.
- The **explanation** (calculation sheet) lists every step: the cargo mass, each leg's distance and
  how it was found, the transport activity, the intensity and its source, the DAF, the emissions, the
  total and the data quality. Each step shows its arithmetic with the numbers used and the GLEC v3.2
  page and ISO 14083 clause it follows, so it can be redone by hand.

## Errors

- **Input errors** (unknown vehicle type, a point off the network, missing distance, impossible
  load): fix the input. The message says what is wrong.
- **Retryable errors:** a routing service didn't answer after retries. Send the same request again.
- In a batch, a failing shipment doesn't stop the others; each failure is listed with its reason.

## Presenting results

- Give the WTW total with its TTW/WTT split, per leg where it helps, and the distances used.
- Say which inputs were assumed (vehicle type, load, added road legs, the container-ship factor
  chosen) and the data type.
- Say that these are calculations following GLEC v3.2 / ISO 14083, not certified figures. Do not state
  ISO 14083 conformity on the user's behalf.

## The public service

At https://carbonara-co2.org, without an account: single shipments and batches of up to 100
shipments, 2,000 shipments a day per visitor. A free account (email sign-in) gives an API key for
100,000 shipments a day and batches of up to 10,000.
