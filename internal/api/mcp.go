package api

// MCP: the Model Context Protocol (https://modelcontextprotocol.io) at
// /mcp, so AI assistants can calculate with Carbonara and see how every
// number was found. It speaks the Streamable HTTP transport without
// sessions: each POST carries a JSON-RPC message (or a batch of them) and
// gets the answers back as JSON; there is no server-sent event stream.
//
// Tools: calculate_shipment (with the full calculation sheet),
// calculate_shipments, emissions_report, find_places, list_vehicle_types
// and get_methodology. The methodology is also a resource.

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/majidkabir/carbonara/pkg/calc"
	"github.com/majidkabir/carbonara/pkg/explain"
	"github.com/majidkabir/carbonara/pkg/factors"
	"github.com/majidkabir/carbonara/pkg/places"
	"github.com/majidkabir/carbonara/pkg/report"
)

//go:embed methodology.md
var methodologyMD string

// MCPPath is where the MCP endpoint is served.
const MCPPath = "/mcp"

// mcpVersions are the protocol revisions understood; the first is offered
// when the client asks for another.
var mcpVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// maxExplained bounds the calculation sheets in one calculate_shipments
// answer: they are long, and an assistant reads every word.
const maxExplained = 25

const methodologyURI = "carbonara://methodology"

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, errors.New(
			"this is an MCP server (Streamable HTTP, no sessions): POST JSON-RPC messages to this URL"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{rpcInvalidRequest, "request body too large"}})
		return
	}
	body = bytes.TrimSpace(body)
	var msgs []rpcMessage
	batch := len(body) > 0 && body[0] == '['
	if batch {
		err = json.Unmarshal(body, &msgs)
	} else {
		var m rpcMessage
		err = json.Unmarshal(body, &m)
		msgs = []rpcMessage{m}
	}
	if err != nil || len(msgs) == 0 {
		writeJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{rpcParseError, "not a JSON-RPC 2.0 message"}})
		return
	}
	var out []rpcResponse
	for _, m := range msgs {
		if len(m.ID) == 0 || m.Method == "" {
			continue // a notification, or a response to us: nothing to answer
		}
		out = append(out, s.rpc(r.Context(), m))
	}
	switch {
	case len(out) == 0:
		w.WriteHeader(http.StatusAccepted)
	case batch:
		writeJSON(w, http.StatusOK, out)
	default:
		writeJSON(w, http.StatusOK, out[0])
	}
}

func (s *Server) rpc(ctx context.Context, m rpcMessage) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: m.ID}
	fail := func(code int, msg string) rpcResponse {
		resp.Error = &rpcError{code, msg}
		return resp
	}
	if m.JSONRPC != "2.0" {
		return fail(rpcInvalidRequest, `jsonrpc must be "2.0"`)
	}
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		v := mcpVersions[0]
		for _, known := range mcpVersions {
			if p.ProtocolVersion == known {
				v = known
			}
		}
		version := s.Version
		if version == "" {
			version = "dev"
		}
		resp.Result = map[string]any{
			"protocolVersion": v,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{"name": "carbonara", "title": "Carbonara freight emissions", "version": version,
				"websiteUrl": "https://carbonara-co2.org"},
			"instructions": mcpInstructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return fail(rpcInvalidParams, "params must be {name, arguments}")
		}
		tool, ok := s.mcpHandlers()[p.Name]
		if !ok {
			return fail(rpcInvalidParams, fmt.Sprintf("unknown tool %q; see tools/list", p.Name))
		}
		if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
			p.Arguments = json.RawMessage("{}")
		}
		resp.Result = tool(ctx, p.Arguments)
	case "resources/list":
		resp.Result = map[string]any{"resources": []map[string]any{{
			"uri": methodologyURI, "name": "methodology", "title": "How Carbonara calculates",
			"description": "Methods, distances, DAFs, emission factors and their sources, data quality, result fields, limitations.",
			"mimeType":    "text/markdown"}}}
	case "resources/templates/list":
		resp.Result = map[string]any{"resourceTemplates": []any{}}
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.URI != methodologyURI {
			return fail(rpcInvalidParams, fmt.Sprintf("unknown resource %q", p.URI))
		}
		resp.Result = map[string]any{"contents": []map[string]any{{
			"uri": methodologyURI, "mimeType": "text/markdown", "text": s.methodology()}}}
	case "prompts/list":
		resp.Result = map[string]any{"prompts": []any{}}
	default:
		return fail(rpcMethodNotFound, fmt.Sprintf("method %q not found", m.Method))
	}
	return resp
}

const mcpInstructions = `Carbonara calculates freight transport greenhouse gas emissions (kg CO2e) following the GLEC Framework v3.2 and ISO 14083:2023.

How to use it:
1. For each leg of the trip you need the mode (road, rail, sea, air, inland_waterway), a vehicle type, and where it goes. Use find_places to turn place names into coordinates or UN/LOCODE/IATA codes; Carbonara routes the distance itself (roads, railways, waterways, sea lanes). Use list_vehicle_types to pick vehicle types; the short aliases (hgv, hgv_artic_33t, van, freight_train, container_ship, container_barge, air_freight, roro, ...) cover most cases.
2. Call calculate_shipment for one shipment, calculate_shipments for several, emissions_report for an ISO 14083 style report over many.
3. Every result says how it was calculated: the distance and how it was found, the factor and its source, any distance adjustment, the method and data type, notes on road legs Carbonara added or ends it moved to a port or terminal, and warnings. calculate_shipment adds a step-by-step calculation sheet with the GLEC page and ISO clause behind each step. Read it, and show the user the parts that matter.

Be careful with:
- Truck load: load_factor is a share of DESNZ's average full load (17.9 t for an articulated truck), not of the truck's rated payload as in GLEC or EcoTransIT. If the user knows the tonnes on the truck, pass load_tonnes instead.
- Assumptions: tell the user which vehicle types and loads you chose and why.
- Results are calculations following GLEC/ISO 14083, not certified figures; never declare ISO 14083 conformity for the user.
get_methodology explains everything in detail.`

// toolResult is an MCP CallToolResult.
type toolResult struct {
	Content           []toolContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func toolError(format string, a ...any) toolResult {
	return toolResult{Content: []toolContent{{"text", fmt.Sprintf(format, a...)}}, IsError: true}
}

// toolOK answers with text for the assistant to read and the same data as
// structured content (and as JSON text, for clients that only pass text on).
func toolOK(text string, structured any) toolResult {
	res := toolResult{Content: []toolContent{{"text", text}}, StructuredContent: structured}
	if structured != nil {
		if b, err := json.Marshal(structured); err == nil {
			res.Content = append(res.Content, toolContent{"text", string(b)})
		}
	}
	return res
}

type toolFunc func(ctx context.Context, args json.RawMessage) toolResult

func (s *Server) mcpHandlers() map[string]toolFunc {
	return map[string]toolFunc{
		"calculate_shipment":  s.toolCalculate,
		"calculate_shipments": s.toolCalculateMany,
		"emissions_report":    s.toolReport,
		"find_places":         s.toolPlaces,
		"list_vehicle_types":  s.toolVehicles,
		"get_methodology": func(context.Context, json.RawMessage) toolResult {
			return toolOK(s.methodology(), nil)
		},
	}
}

// decodeArgs reads tool arguments strictly: an unknown field is an error,
// so a misspelt one can't silently change a result.
func decodeArgs(args json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// calcCtx applies the server's concurrency cap and request timeout to a
// calculation tool; release must be called when done.
func (s *Server) calcCtx(ctx context.Context, roadLegs *bool) (context.Context, func(), error) {
	select {
	case s.sem <- struct{}{}:
	default:
		return nil, nil, errors.New("the server is busy; try again in a few seconds")
	}
	ctx, cancel := context.WithTimeout(ctx, s.Limits.Timeout)
	if roadLegs != nil && !*roadLegs {
		ctx = calc.WithoutRoadLegs(ctx)
	}
	return ctx, func() { cancel(); <-s.sem }, nil
}

// failText says what to do about a calculation error.
func failText(err error) string {
	if calc.Temporary(err) {
		return "Temporary failure (a routing service didn't answer); send the same request again: " + err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The calculation took too long and was stopped; try fewer shipments per call: " + err.Error()
	}
	return "The input can't be calculated as given; fix it and try again: " + err.Error()
}

// ---- calculate_shipment ----

func (s *Server) toolCalculate(ctx context.Context, args json.RawMessage) toolResult {
	var sh calc.Shipment
	if err := decodeArgs(args, &sh); err != nil {
		return toolError("%v. Arguments are one shipment: see the tool's input schema.", err)
	}
	if err := s.checkSize(sh); err != nil {
		return toolError("%v", err)
	}
	ctx, done, err := s.calcCtx(ctx, nil)
	if err != nil {
		return toolError("%v", err)
	}
	defer done()
	res, err := s.Calc.CalculateRetrying(ctx, sh)
	if err != nil {
		return toolError("%s", failText(err))
	}
	sheet := explain.Shipment(sh, res, s.Calc.Factors)
	text := resultSummary(res) + "\n\n" + explain.Markdown(sheet)
	return toolOK(text, map[string]any{"result": res, "explanation": sheet})
}

// resultSummary is a short, complete account of a result for an assistant.
func resultSummary(r calc.Result) string {
	var b strings.Builder
	id := r.ShipmentID
	if id == "" {
		id = "shipment"
	}
	fmt.Fprintf(&b, "## %s: %s kg CO2e well-to-wheel\n\n", id, num(r.WTWKg))
	fmt.Fprintf(&b, "- Tank-to-wheel (vehicle) %s kg, well-to-tank (energy supply) %s kg\n", num(r.TTWKg), num(r.WTTKg))
	fmt.Fprintf(&b, "- %s t over %s tonne-km: %s g CO2e per tonne-km (transport)\n", num(r.WeightKg/1000), num(r.TonneKm), num(r.IntensityG))
	fmt.Fprintf(&b, "- Primary data share %.0f%%; factors: %s\n", r.PrimaryDataShare*100, r.Dataset)
	for i, l := range r.Legs {
		fmt.Fprintf(&b, "- Leg %d (%s, %s): %s km (%s, %s) → %s kg WTW; method %s, %s data",
			i+1, l.Mode, l.VehicleType, num(l.DistanceKm), l.DistanceMethod, orNone(l.DistanceType), num(l.WTWKg), l.Method, l.DataType)
		if l.Factor != nil {
			fmt.Fprintf(&b, "; factor %s (%s g/tkm WTW)", l.Factor.VehicleType, num(l.Factor.WTW()))
		}
		if l.DAF != 0 {
			fmt.Fprintf(&b, "; DAF %.3f", l.DAF)
		}
		if l.Added != "" {
			fmt.Fprintf(&b, "; added by Carbonara (%s)", l.Added)
		}
		b.WriteString("\n")
		for _, n := range []string{l.Note, l.FactorNote} {
			if n != "" {
				fmt.Fprintf(&b, "  - %s\n", n)
			}
		}
	}
	for _, h := range r.Hubs {
		fmt.Fprintf(&b, "- Hub %s (%s): %s kg WTW\n", h.HubID, h.HubType, num(h.WTWKg))
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "- Warning: %s\n", w)
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "type not given"
	}
	return s
}

// num formats a number for reading: thousands separated, sensible decimals.
func num(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Sprint(v)
	}
	dec := 1
	switch a := math.Abs(v); {
	case a >= 100:
		dec = 0
	case a < 1:
		dec = 3
	}
	s := fmt.Sprintf("%.*f", dec, v)
	intPart, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var g []string
	for len(intPart) > 3 {
		g = append([]string{intPart[len(intPart)-3:]}, g...)
		intPart = intPart[:len(intPart)-3]
	}
	g = append([]string{intPart}, g...)
	out := strings.Join(g, ",")
	if neg {
		out = "-" + out
	}
	if frac != "" {
		out += "." + frac
	}
	return out
}

// ---- calculate_shipments ----

type manyArgs struct {
	Shipments   []calc.Shipment `json:"shipments"`
	AddRoadLegs *bool           `json:"add_road_legs,omitempty"`
	Explain     bool            `json:"explain,omitempty"`
}

type manyItem struct {
	ShipmentID  string         `json:"shipment_id"`
	Result      *calc.Result   `json:"result,omitempty"`
	Explanation *explain.Sheet `json:"explanation,omitempty"`
	Error       string         `json:"error,omitempty"`
	Retryable   bool           `json:"retryable,omitempty"`
}

type manyTotals struct {
	WTWKg   float64 `json:"wtw_kg"`
	TTWKg   float64 `json:"ttw_kg"`
	WTTKg   float64 `json:"wtt_kg"`
	TonneKm float64 `json:"tonne_km"`
}

func (s *Server) toolCalculateMany(ctx context.Context, args json.RawMessage) toolResult {
	var a manyArgs
	if err := decodeArgs(args, &a); err != nil {
		return toolError("%v. Arguments are {\"shipments\": [...], \"add_road_legs\": true, \"explain\": false}.", err)
	}
	if len(a.Shipments) == 0 {
		return toolError("shipments is empty")
	}
	ctx, done, err := s.calcCtx(ctx, a.AddRoadLegs)
	if err != nil {
		return toolError("%v", err)
	}
	defer done()
	outs, err := s.calcAll(ctx, a.Shipments)
	if err != nil {
		return toolError("%s", failText(err))
	}
	var items []manyItem
	var tot manyTotals
	failed, retryable, explained := 0, 0, 0
	var b strings.Builder
	for i, o := range outs {
		sh := a.Shipments[i]
		it := manyItem{ShipmentID: sh.ID}
		if o.err != nil {
			it.Error, it.Retryable = o.err.Error(), calc.Temporary(o.err)
			failed++
			if it.Retryable {
				retryable++
			}
		} else {
			res := o.res
			it.Result = &res
			tot.WTWKg += res.WTWKg
			tot.TTWKg += res.TTWKg
			tot.WTTKg += res.WTTKg
			tot.TonneKm += res.TonneKm
			if a.Explain && explained < maxExplained {
				sheet := explain.Shipment(sh, res, s.Calc.Factors)
				it.Explanation = &sheet
				explained++
			}
		}
		items = append(items, it)
	}
	fmt.Fprintf(&b, "# %d shipments: %s kg CO2e well-to-wheel\n\n", len(outs), num(tot.WTWKg))
	fmt.Fprintf(&b, "Calculated %d, failed %d", len(outs)-failed, failed)
	if retryable > 0 {
		fmt.Fprintf(&b, " (%d temporary: send those again)", retryable)
	}
	fmt.Fprintf(&b, ". Total TTW %s kg, WTT %s kg, %s tonne-km.\n\n", num(tot.TTWKg), num(tot.WTTKg), num(tot.TonneKm))
	for _, it := range items {
		if it.Error != "" {
			fmt.Fprintf(&b, "## %s: failed\n\n%s\n\n", orID(it.ShipmentID), failText(errors.New(it.Error)))
			continue
		}
		b.WriteString(resultSummary(*it.Result))
		b.WriteString("\n")
	}
	if a.Explain && explained < len(outs)-failed {
		fmt.Fprintf(&b, "Calculation sheets are included for the first %d shipments only (structured content).\n", maxExplained)
	} else if !a.Explain {
		b.WriteString("For the step-by-step calculation of a shipment, call calculate_shipment for it, or this tool with \"explain\": true.\n")
	}
	return toolOK(b.String(), map[string]any{"shipments": len(outs), "calculated": len(outs) - failed, "failed": failed,
		"retryable": retryable, "total": tot, "results": items})
}

func orID(id string) string {
	if id == "" {
		return "(no id)"
	}
	return id
}

// ---- emissions_report ----

type reportArgs struct {
	report.Meta
	Shipments   []calc.Shipment `json:"shipments"`
	AddRoadLegs *bool           `json:"add_road_legs,omitempty"`
}

func (s *Server) toolReport(ctx context.Context, args json.RawMessage) toolResult {
	var a reportArgs
	if err := decodeArgs(args, &a); err != nil {
		return toolError("%v", err)
	}
	if len(a.Shipments) == 0 {
		return toolError("shipments is empty")
	}
	if err := checkMeta(a.Meta); err != nil {
		return toolError("%v", err)
	}
	ctx, done, err := s.calcCtx(ctx, a.AddRoadLegs)
	if err != nil {
		return toolError("%v", err)
	}
	defer done()
	outs, err := s.calcAll(ctx, a.Shipments)
	if err != nil {
		return toolError("%s", failText(err))
	}
	bld := report.New(a.Meta)
	for i, o := range outs {
		bld.Add(a.Shipments[i].ID, o.res, o.err)
	}
	rep := bld.Report()
	var md bytes.Buffer
	if err := report.WriteMarkdown(&md, rep); err != nil {
		return toolError("%v", err)
	}
	return toolOK(md.String(), rep)
}

// ---- find_places ----

func (s *Server) toolPlaces(_ context.Context, args json.RawMessage) toolResult {
	var a struct {
		Query string   `json:"query"`
		Kinds []string `json:"kinds,omitempty"`
		Limit int      `json:"limit,omitempty"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return toolError("%v", err)
	}
	if strings.TrimSpace(a.Query) == "" || len(a.Query) > 100 {
		return toolError("query must be 1 to 100 characters: a place name, UN/LOCODE or IATA code")
	}
	load := s.Places
	if load == nil {
		load = places.Default
	}
	ix, err := load()
	if err != nil {
		return toolError("the place list isn't available: %v", err)
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}
	found := ix.Search(a.Query, a.Kinds, min(limit, 50))
	if found == nil {
		found = []places.Place{}
	}
	var b strings.Builder
	if len(found) == 0 {
		fmt.Fprintf(&b, "No places match %q", a.Query)
		if len(a.Kinds) > 0 {
			fmt.Fprintf(&b, " of kinds %s", strings.Join(a.Kinds, ", "))
		}
		b.WriteString(". Try another spelling, the English name, or fewer kinds.\n")
	}
	for _, p := range found {
		name := p.Name
		if p.AltName != "" {
			name += " (" + p.AltName + ")"
		}
		code := p.Code
		if code == "" {
			code = "-"
		}
		fmt.Fprintf(&b, "- %s, %s [%s]: code %s, lat %.4f, lon %.4f\n", name, p.Country, strings.Join(p.Kinds, ", "), code, p.Lat, p.Lon)
	}
	b.WriteString("\nUse a place as {\"code\": \"...\"} where it has a code, otherwise {\"lat\": ..., \"lon\": ..., \"name\": \"...\"}.\n")
	return toolOK(b.String(), map[string]any{"places": found})
}

// ---- list_vehicle_types ----

type vehicleInfo struct {
	Mode          calc.Mode `json:"mode"`
	VehicleType   string    `json:"vehicle_type"`
	Aliases       []string  `json:"aliases,omitempty"`
	TTW           float64   `json:"ttw_g_per_tkm"`
	WTT           float64   `json:"wtt_g_per_tkm"`
	WTW           float64   `json:"wtw_g_per_tkm"`
	DistanceBasis string    `json:"distance_basis,omitempty"`
	Source        string    `json:"source"`
	Year          int       `json:"year"`
	// PayloadT is the load model's payload, when the vehicle has one: what
	// load_factor 1 means.
	PayloadT float64 `json:"load_model_payload_t,omitempty"`
}

func (s *Server) toolVehicles(_ context.Context, args json.RawMessage) toolResult {
	var a struct {
		Mode         string `json:"mode,omitempty"`
		Search       string `json:"search,omitempty"`
		IncludeFuels bool   `json:"include_fuels,omitempty"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return toolError("%v", err)
	}
	ds, ok := s.Factors.(*factors.Dataset)
	if !ok {
		return toolError("this server doesn't list its factors")
	}
	search := strings.ToLower(strings.TrimSpace(a.Search))
	aliases := map[string][]string{}
	var aliasLines []string
	for _, al := range ds.VehicleAliases {
		k := string(al.Mode) + "/" + al.VehicleType
		aliases[k] = append(aliases[k], al.Alias)
		if a.Mode != "" && string(al.Mode) != a.Mode {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(al.Alias+" "+al.VehicleType), search) {
			continue
		}
		line := fmt.Sprintf("- %s `%s` → %s", al.Mode, al.Alias, al.VehicleType)
		if al.ShortVehicleType != "" {
			line += fmt.Sprintf(" (legs under %.0f km: %s)", al.ShortBelowKm, al.ShortVehicleType)
		}
		aliasLines = append(aliasLines, line)
	}
	payload := map[string]float64{}
	for _, m := range ds.LoadModels {
		payload[string(m.Mode)+"/"+m.VehicleType] = m.PayloadT
	}
	var list []vehicleInfo
	for _, f := range ds.Legs {
		if a.Mode != "" && string(f.Mode) != a.Mode {
			continue
		}
		k := string(f.Mode) + "/" + f.VehicleType
		if search != "" && !strings.Contains(strings.ToLower(f.VehicleType+" "+strings.Join(aliases[k], " ")+" "+f.Source), search) {
			continue
		}
		v := vehicleInfo{Mode: f.Mode, VehicleType: f.VehicleType, Aliases: aliases[k], TTW: f.TTW, WTT: f.WTT, WTW: f.WTW(),
			DistanceBasis: f.DistanceBasis, Source: f.Source, Year: f.Year}
		for class, p := range payload { // load models are per class: a prefix of the vehicle type
			if k == class || strings.HasPrefix(k, class+"/") {
				v.PayloadT = p
			}
		}
		list = append(list, v)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if (len(list[i].Aliases) > 0) != (len(list[j].Aliases) > 0) {
			return len(list[i].Aliases) > 0 // aliased first
		}
		return false
	})
	var b strings.Builder
	b.WriteString("# Vehicle types\n\nShort aliases (use these unless the user needs something specific):\n\n")
	b.WriteString(strings.Join(aliasLines, "\n"))
	fmt.Fprintf(&b, "\n\n%d vehicle types", len(list))
	if a.Mode != "" {
		fmt.Fprintf(&b, " for %s", a.Mode)
	}
	if search != "" {
		fmt.Fprintf(&b, " matching %q", a.Search)
	}
	b.WriteString(" (g CO2e per tonne-km; TTW + WTT = WTW):\n\n")
	for _, v := range list {
		fmt.Fprintf(&b, "- %s `%s`: %s + %s = %s", v.Mode, v.VehicleType, num(v.TTW), num(v.WTT), num(v.WTW))
		if v.DistanceBasis != "" {
			fmt.Fprintf(&b, " per %s distance", v.DistanceBasis)
		}
		if v.PayloadT > 0 {
			fmt.Fprintf(&b, "; load model, load_factor 1 = %.1f t", v.PayloadT)
		}
		fmt.Fprintf(&b, "; %s (%d)\n", v.Source, v.Year)
	}
	out := map[string]any{"dataset": ds.Name, "vehicle_types": list, "dafs": ds.DAFs}
	if a.IncludeFuels {
		b.WriteString("\nFuels (kg CO2e per unit; for the fuel method):\n\n")
		for _, f := range ds.Fuels {
			if search != "" && !strings.Contains(strings.ToLower(f.Fuel), search) {
				continue
			}
			fmt.Fprintf(&b, "- `%s` per %s: TTW %g + WTT %g\n", f.Fuel, f.Unit, f.TTW, f.WTT)
		}
		out["fuels"] = ds.Fuels
		out["fuel_aliases"] = ds.FuelAliases
	}
	return toolOK(b.String(), out)
}

// ---- methodology ----

// methodology is the method document plus how this server routes.
func (s *Server) methodology() string {
	var b strings.Builder
	b.WriteString(methodologyMD)
	b.WriteString("\n## This server\n\n")
	if ds, ok := s.Factors.(*factors.Dataset); ok {
		fmt.Fprintf(&b, "- Emission factor dataset: `%s`.\n", ds.Name)
	}
	if dm, ok := s.Calc.Distances.(calc.DistanceMethoder); ok {
		for _, m := range []calc.Mode{calc.Road, calc.Rail, calc.InlandWaterway, calc.Sea, calc.Air} {
			how := dm.DistanceMethod(m)
			if how == "" {
				how = "not available: give distance_km"
			}
			fmt.Fprintf(&b, "- %s distances: %s\n", m, how)
		}
	}
	return b.String()
}

// checkMeta validates a report's free-text fields and dates.
func checkMeta(m report.Meta) error {
	for _, f := range []string{m.Title, m.Organisation, m.Reference, m.PeriodFrom, m.PeriodTo} {
		if len(f) > maxMetaLen {
			return fmt.Errorf("report text fields are limited to %d bytes", maxMetaLen)
		}
	}
	for _, d := range []string{m.PeriodFrom, m.PeriodTo} {
		if d != "" && !validDate(d) {
			return fmt.Errorf("period dates must be YYYY-MM-DD, got %q", d)
		}
	}
	return nil
}
