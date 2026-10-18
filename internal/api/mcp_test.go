package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// rpc posts a JSON-RPC body to /mcp and decodes the answer.
func rpc(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url+MCPPath, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// call runs a tool and returns its result.
func call(t *testing.T, url, tool, args string) map[string]any {
	t.Helper()
	code, out := rpc(t, url, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"`+tool+`","arguments":`+args+`}}`)
	if code != http.StatusOK || out["error"] != nil {
		t.Fatalf("%s: %d %v", tool, code, out)
	}
	return out["result"].(map[string]any)
}

func text(res map[string]any) string {
	var b strings.Builder
	for _, c := range res["content"].([]any) {
		b.WriteString(c.(map[string]any)["text"].(string))
		b.WriteString("\n")
	}
	return b.String()
}

func TestMCP_Handshake(t *testing.T) {
	srv := newTestServer(t)
	code, out := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	res, _ := out["result"].(map[string]any)
	if code != http.StatusOK || res["protocolVersion"] != "2025-03-26" || out["id"].(float64) != 1 {
		t.Fatalf("initialize: %d %v", code, out)
	}
	if info := res["serverInfo"].(map[string]any); info["name"] != "carbonara" {
		t.Errorf("serverInfo %v", info)
	}
	if !strings.Contains(res["instructions"].(string), "load_tonnes") {
		t.Error("instructions don't explain the load factor")
	}
	if _, out := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`); out["result"].(map[string]any)["protocolVersion"] != mcpVersions[0] {
		t.Errorf("unknown version: want the latest, got %v", out)
	}
	// A notification gets 202 and no body.
	resp, err := http.Post(srv.URL+MCPPath, "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Errorf("notification: %v %v", resp.StatusCode, err)
	}
	if code, out := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":3,"method":"ping"}`); code != 200 || out["result"] == nil {
		t.Errorf("ping: %v", out)
	}
	if _, out := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":4,"method":"nope"}`); out["error"].(map[string]any)["code"].(float64) != rpcMethodNotFound {
		t.Errorf("unknown method: %v", out)
	}
	if code, _ := rpc(t, srv.URL, `{not json`); code != http.StatusBadRequest {
		t.Errorf("parse error: %d", code)
	}
	resp, err = http.Get(srv.URL + MCPPath)
	if err != nil || resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: %v %v", resp.StatusCode, err)
	}
	// A batch gets a batch.
	resp, err = http.Post(srv.URL+MCPPath, "application/json", strings.NewReader(
		`[{"jsonrpc":"2.0","id":"a","method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":"b","method":"tools/list"}]`))
	if err != nil {
		t.Fatal(err)
	}
	var batch []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&batch)
	resp.Body.Close()
	if len(batch) != 2 || batch[0]["id"] != "a" || batch[1]["id"] != "b" {
		t.Errorf("batch: %v", batch)
	}
}

func TestMCP_ToolsList(t *testing.T) {
	srv := newTestServer(t)
	_, out := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, x := range tools {
		tool := x.(map[string]any)
		names[tool["name"].(string)] = true
		if tool["inputSchema"].(map[string]any)["type"] != "object" || tool["description"] == "" {
			t.Errorf("tool %v", tool["name"])
		}
	}
	for _, n := range []string{"calculate_shipment", "calculate_shipments", "emissions_report", "find_places", "list_vehicle_types", "get_methodology"} {
		if !names[n] {
			t.Errorf("missing tool %s", n)
		}
	}
	if len(tools) != 6 {
		t.Errorf("%d tools", len(tools))
	}
}

func TestMCP_CalculateShipment(t *testing.T) {
	srv := newTestServer(t)
	// sample truck_40t: 60 + 15 g/tkm. 10 t x 500 km -> 375 kg WTW
	res := call(t, srv.URL, "calculate_shipment",
		`{"id":"s1","weight_kg":10000,"legs":[{"id":"l1","mode":"road","vehicle_type":"truck_40t","distance_km":500}]}`)
	if res["isError"] == true {
		t.Fatalf("error: %v", text(res))
	}
	sc := res["structuredContent"].(map[string]any)
	if sc["result"].(map[string]any)["wtw_kg"].(float64) != 375 {
		t.Errorf("wtw %v", sc["result"])
	}
	if steps := sc["explanation"].(map[string]any)["steps"].([]any); len(steps) < 5 {
		t.Errorf("explanation has %d steps", len(steps))
	}
	txt := text(res)
	for _, want := range []string{"## s1: 375 kg CO2e well-to-wheel", "Tank-to-wheel (vehicle) 300 kg", "# Calculation: s1",
		"### Emission intensity", "ISO 14083", "```"} {
		if !strings.Contains(txt, want) {
			t.Errorf("text lacks %q:\n%s", want, txt)
		}
	}

	// Mistakes come back as tool errors the assistant can read.
	for args, want := range map[string]string{
		`{"weight_kg":1000,"legs":[{"mode":"road","vehicle_type":"truck_40t","distance_km":10,"lenght":3}]}`: "unknown field",
		`{"weight_kg":1000,"legs":[{"mode":"road","vehicle_type":"no_such","distance_km":10}]}`:              "fix it",
	} {
		res := call(t, srv.URL, "calculate_shipment", args)
		if res["isError"] != true || !strings.Contains(text(res), want) {
			t.Errorf("%s: want error with %q, got %v", args, want, text(res))
		}
	}
	if _, out := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`); out["error"] == nil {
		t.Error("unknown tool accepted")
	}
}

func TestMCP_ManyAndReport(t *testing.T) {
	srv := newTestServer(t)
	ships := `[{"id":"a","weight_kg":10000,"legs":[{"mode":"road","vehicle_type":"truck_40t","distance_km":500}]},
	           {"id":"b","weight_kg":1000,"legs":[{"mode":"road","vehicle_type":"no_such","distance_km":10}]}]`
	res := call(t, srv.URL, "calculate_shipments", `{"shipments":`+ships+`,"explain":true}`)
	sc := res["structuredContent"].(map[string]any)
	if sc["calculated"].(float64) != 1 || sc["failed"].(float64) != 1 || sc["total"].(map[string]any)["wtw_kg"].(float64) != 375 {
		t.Errorf("many: %v", sc)
	}
	items := sc["results"].([]any)
	if items[0].(map[string]any)["explanation"] == nil || items[1].(map[string]any)["error"] == "" {
		t.Errorf("items: %v", items)
	}
	if txt := text(res); !strings.Contains(txt, "## b: failed") || !strings.Contains(txt, "## a: 375 kg") {
		t.Errorf("text: %s", txt)
	}

	rep := call(t, srv.URL, "emissions_report", `{"organisation":"Acme","period_from":"2026-01-01","shipments":`+ships+`}`)
	if rep["isError"] == true || !strings.Contains(text(rep), "Acme") {
		t.Errorf("report: %v", text(rep))
	}
	if bad := call(t, srv.URL, "emissions_report", `{"period_from":"January","shipments":`+ships+`}`); bad["isError"] != true {
		t.Error("bad date accepted")
	}
}

func TestMCP_Lookups(t *testing.T) {
	srv := newTestServer(t)
	res := call(t, srv.URL, "find_places", `{"query":"Rotterdam","kinds":["port"],"limit":3}`)
	if txt := text(res); !strings.Contains(txt, "NLRTM") {
		t.Errorf("find_places: %s", txt)
	}
	res = call(t, srv.URL, "list_vehicle_types", `{"mode":"road","include_fuels":true}`)
	if txt := text(res); !strings.Contains(txt, "truck_40t") || !strings.Contains(txt, "Fuels") {
		t.Errorf("list_vehicle_types: %s", txt)
	}
	res = call(t, srv.URL, "get_methodology", `{}`)
	if txt := text(res); !strings.Contains(txt, "## Distance adjustment factors") && !strings.Contains(txt, "### Distance adjustment factors") || !strings.Contains(txt, "## This server") {
		t.Errorf("methodology: %.300s", txt)
	}
	_, out := rpc(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"carbonara://methodology"}}`)
	if c := out["result"].(map[string]any)["contents"].([]any); !strings.Contains(c[0].(map[string]any)["text"].(string), "load_factor") {
		t.Errorf("resource: %v", out)
	}
}
