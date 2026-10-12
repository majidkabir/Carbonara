# Carbonara 🍝

Open-source freight emissions calculator following the GLEC Framework / ISO 14083 approach.

> ⚠️ The embedded `sample` factors are **placeholders**. Load real GLEC or DEFRA factors with `-factors` before using results.

## Quick start

```sh
go test ./...
go run ./cmd/carbonara calc examples/cph-rotterdam.json
go run ./cmd/carbonara calc -factors my-factors.json examples/cph-rotterdam.json
```

## Layout

| Path | What |
|---|---|
| `pkg/calc` | Domain model + calculation. Stdlib only. Defines `FactorProvider` and `DistanceProvider`. |
| `pkg/factors` | JSON factor datasets, embedded via `go:embed` or loaded from file. |
| `pkg/distance` | Great-circle distance with per-mode adjustment factors. |
| `cmd/carbonara` | CLI. |

## Calculation

```
tonne-km = weight_t × distance_km
TTW kg   = tonne-km × TTW g/tkm / 1000
WTW kg   = tonne-km × (TTW + WTT) g/tkm / 1000
hub kg   = weight_t × hub g/t / 1000      (WTW, added to total)
intensity = transport WTW g / tonne-km    (hubs excluded)
```

## Roadmap

- [x] Core model, intensity-based calculation, hubs
- [x] Factor datasets (embedded + file)
- [x] Great-circle distance
- [x] CLI (`calc`, JSON in/out)
- [ ] Real GLEC/DEFRA datasets + GLEC distance adjustment factors
- [ ] GLEC worked examples as golden tests
- [ ] Fuel-based calculation path
- [ ] Batch CSV input
- [ ] HTTP API + OpenAPI spec
- [ ] OSRM road routing, SeaRoute network for sea
- [ ] GoReleaser + Docker image

## License

Apache 2.0 (to be added).
