package api

// The MCP tools' definitions: names, descriptions and JSON Schemas for
// their arguments. The descriptions are what an assistant reads to decide
// how to call a tool, so they say what each field means in this
// calculation, not just its type.

type obj = map[string]any

func numberProp(desc string) obj { return obj{"type": "number", "description": desc} }
func stringProp(desc string) obj { return obj{"type": "string", "description": desc} }

var locationSchema = obj{
	"type": "object",
	"description": "A place: coordinates (lat and lon, optionally name), or a code instead. " +
		"Use find_places to look up coordinates or codes for a place name.",
	"properties": obj{
		"lat":  obj{"type": "number", "minimum": -90, "maximum": 90, "description": "Latitude, decimal degrees"},
		"lon":  obj{"type": "number", "minimum": -180, "maximum": 180, "description": "Longitude, decimal degrees"},
		"name": stringProp("Label for the place, shown in results"),
		"code": stringProp("Instead of lat/lon: a UN/LOCODE (e.g. NLRTM for Rotterdam) or IATA airport code (e.g. CPH)"),
	},
	"additionalProperties": false,
}

var legSchema = obj{
	"type":        "object",
	"description": "One transport movement with one mode and vehicle type (an ISO 14083 transport chain element).",
	"properties": obj{
		"id":   stringProp("Your label for the leg"),
		"mode": obj{"type": "string", "enum": []string{"road", "rail", "sea", "air", "inland_waterway"}},
		"vehicle_type": stringProp("A short alias or a full id from list_vehicle_types. Common: road hgv (average truck), " +
			"hgv_artic_33t (articulated truck over 33 t, a 40 t semitrailer), hgv_rigid, van; rail freight_train; " +
			"sea container_ship (short-sea factor under 2,000 km, deep-sea from 2,000 km), bulk_carrier, general_cargo, roro, ropax; " +
			"air air_freight; inland_waterway barge, container_barge."),
		"from": locationSchema,
		"to":   locationSchema,
		"distance_km": numberProp("The distance, if known. When given, it is used as is and not routed; " +
			"say what kind it is with distance_type. Leave it out to route from the coordinates (preferred)."),
		"distance_type": obj{"type": "string", "enum": []string{"sfd", "gcd", "actual"},
			"description": "Kind of distance_km: sfd = shortest feasible route, gcd = great circle, actual = actually travelled (e.g. odometer). " +
				"Decides the distance adjustment factor (DAF) applied."},
		"load_tonnes": numberProp("Trucks: the tonnes on the truck (all cargo, averaged over the loaded km), " +
			"which switches the leg to the load model. Prefer this over load_factor when the weight is known."),
		"load_factor": numberProp("Trucks: load as a share (0-1) of the load model's payload, DESNZ's average full load " +
			"(17.9 t for an articulated truck over 33 t), NOT of the truck's rated payload as in GLEC or EcoTransIT. " +
			"Give load_tonnes or load_factor, not both."),
		"empty_trip_factor": numberProp("Trucks, with load_tonnes or load_factor: empty km driven per loaded km (e.g. 0.3)."),
		"fuel": obj{"type": "object", "description": "Measured fuel for the vehicle trip this leg is part of (primary data).",
			"properties": obj{
				"fuel":   stringProp("Fuel id or alias: diesel, petrol, hvo, marine_gas_oil, marine_fuel_oil, jet_fuel (per litre), electricity/<ISO3 country> (kWh), or others from list_vehicle_types with include_fuels"),
				"amount": numberProp("Amount in the fuel's unit (litres for the aliases)"),
				"vehicle_tonne_km": numberProp("Total tonne-km of the whole trip, to give this shipment its share of the fuel. " +
					"Leave out if the trip carried only this shipment."),
			},
			"required": []string{"fuel", "amount"}, "additionalProperties": false},
		"energy_kwh_per_tkm": numberProp("Electric trains or trucks: electricity per tonne-km (the operator's figure; no default), with country."),
		"country":            stringProp("With energy_kwh_per_tkm: ISO 3166 alpha-3 country whose grid supplies the electricity, e.g. DEU"),
	},
	"required":             []string{"mode", "vehicle_type"},
	"additionalProperties": false,
}

var shipmentProps = obj{
	"id":             stringProp("Your reference for the shipment"),
	"weight_kg":      numberProp("Cargo weight in kg (or give teu instead)"),
	"teu":            numberProp("Containers in TEU, instead of weight_kg; needs teu_net_tonnes"),
	"teu_net_tonnes": numberProp("With teu: net cargo tonnes per TEU (container tare is not added)"),
	"container_load": stringProp("With teu: a container load profile defined by the dataset, instead of teu_net_tonnes"),
	"legs": obj{"type": "array", "minItems": 1, "items": legSchema,
		"description": "The legs in order, from where the shipper hands the goods over to where the consignee receives them."},
	"hubs": obj{"type": "array", "description": "Hub stops (transhipment, storage); needs a dataset with hub factors.",
		"items": obj{"type": "object", "properties": obj{"id": stringProp("Hub label"), "hub_type": stringProp("Hub type from the dataset")},
			"required": []string{"hub_type"}, "additionalProperties": false}},
	"add_road_legs": obj{"type": "boolean", "description": "Add road legs where the legs don't join up (gaps over 5 km), and " +
		"to a port or terminal when a sea, rail or river leg starts or ends off its network (default true). Added legs are marked in the result."},
}

func shipmentSchema() obj {
	return obj{"type": "object", "properties": shipmentProps, "required": []string{"legs"}, "additionalProperties": false}
}

var readOnly = obj{"readOnlyHint": true, "idempotentHint": true, "openWorldHint": false}

func mcpTools() []obj {
	many := obj{"type": "array", "minItems": 1, "items": shipmentSchema(),
		"description": "The shipments. Without an API key the public service takes up to 100 per call; with one, up to 10,000."}
	return []obj{
		{
			"name":  "calculate_shipment",
			"title": "Calculate a shipment's emissions",
			"description": "Calculates one shipment's greenhouse gas emissions (kg CO2e: well-to-wheel, tank-to-wheel, well-to-tank) " +
				"following the GLEC Framework v3.2 and ISO 14083. Routes each leg's distance from its coordinates or codes. " +
				"Returns the result per leg (distance and how it was found, method, data type, emission factor and its source, " +
				"distance adjustment, notes on legs Carbonara added) and a step-by-step calculation sheet with the arithmetic and " +
				"the GLEC page and ISO 14083 clause behind each step.",
			"inputSchema": shipmentSchema(),
			"annotations": readOnly,
		},
		{
			"name":  "calculate_shipments",
			"title": "Calculate many shipments",
			"description": "Calculates several shipments at once, each like calculate_shipment. A failing shipment doesn't stop the " +
				"others; each failure says why and whether sending it again can help. Set explain to add calculation sheets " +
				"(first 25 shipments).",
			"inputSchema": obj{"type": "object", "properties": obj{
				"shipments":     many,
				"add_road_legs": shipmentProps["add_road_legs"],
				"explain":       obj{"type": "boolean", "description": "Add the step-by-step calculation sheet of each shipment (up to 25)"},
			}, "required": []string{"shipments"}, "additionalProperties": false},
			"annotations": readOnly,
		},
		{
			"name":  "emissions_report",
			"title": "Emissions report over many shipments",
			"description": "Calculates shipments and returns a report with the basic content ISO 14083 asks for at the level of " +
				"transport services (GLEC v3.2 Section 2 Ch.1): coverage and failures, totals split into TTW and WTT, intensity, " +
				"per mode and hub type, energy carriers, data quality (primary/modelled/default shares, sources, models), " +
				"distance adjustments, and notes. Returned as Markdown and as structured data.",
			"inputSchema": obj{"type": "object", "properties": obj{
				"shipments":     many,
				"title":         stringProp("Report title"),
				"organisation":  stringProp("Organisation the report is for"),
				"period_from":   stringProp("Start of the period covered, YYYY-MM-DD"),
				"period_to":     stringProp("End of the period covered, YYYY-MM-DD"),
				"reference":     stringProp("Where supporting information can be found"),
				"declare_iso":   obj{"type": "boolean", "description": "Add the ISO 14083:2023 conformity statement. Only when the user explicitly confirms that their data collection and boundaries conform."},
				"add_road_legs": shipmentProps["add_road_legs"],
			}, "required": []string{"shipments"}, "additionalProperties": false},
			"annotations": readOnly,
		},
		{
			"name":  "find_places",
			"title": "Find places",
			"description": "Finds cities, sea and inland ports, rail and road terminals, border crossings and airports by name, " +
				"UN/LOCODE or IATA code, with coordinates, from an embedded list (GeoNames, UN/LOCODE, OurAirports). " +
				"Use it to fill a leg's from and to.",
			"inputSchema": obj{"type": "object", "properties": obj{
				"query": stringProp("Place name, UN/LOCODE (NLRTM) or IATA code (CPH)"),
				"kinds": obj{"type": "array", "items": obj{"type": "string", "enum": []string{"city", "port", "seaport", "rail", "road", "airport", "border"}},
					"description": "Only places of these kinds, e.g. [\"seaport\"] for a sea leg, [\"airport\"] for an air leg"},
				"limit": obj{"type": "integer", "minimum": 1, "maximum": 50, "description": "Most results (default 10)"},
			}, "required": []string{"query"}, "additionalProperties": false},
			"annotations": readOnly,
		},
		{
			"name":  "list_vehicle_types",
			"title": "List vehicle types and emission factors",
			"description": "Lists the vehicle types (and their short aliases) with their emission factors: g CO2e per tonne-km " +
				"tank-to-wheel and well-to-tank, the distance type they are per, source and year, and for trucks the load model's " +
				"payload (what load_factor 1 means). Optionally the fuels for the fuel method.",
			"inputSchema": obj{"type": "object", "properties": obj{
				"mode":          obj{"type": "string", "enum": []string{"road", "rail", "sea", "air", "inland_waterway"}},
				"search":        stringProp("Only vehicle types whose id, alias or source contains this, e.g. \"container\" or \"refrigerated\""),
				"include_fuels": obj{"type": "boolean", "description": "Also list fuels and their factors"},
			}, "additionalProperties": false},
			"annotations": readOnly,
		},
		{
			"name":  "get_methodology",
			"title": "How Carbonara calculates",
			"description": "Explains the whole method: scope (WTW, TTW, WTT), inputs, the four calculation methods with formulas, " +
				"how distances are routed, distance adjustment factors, added road legs, the emission factor datasets and their " +
				"sources and limitations, data quality, what every result field means, errors, and how to present results.",
			"inputSchema": obj{"type": "object", "additionalProperties": false},
			"annotations": readOnly,
		},
	}
}
