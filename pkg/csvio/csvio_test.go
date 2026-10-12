package csvio

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/majidkabir/carbonara/pkg/calc"
)

const input = `shipment_id,weight_kg,leg_id,mode,vehicle_type,distance_km,from_lat,from_lon,to_lat,to_lon,fuel,fuel_amount,vehicle_tonne_km,hub_id,hub_type
S1,10000,L1,road,truck_40t,500,,,,,,,,,
S1,,L2,sea,container,,53.55,9.99,51.92,4.48,,,,,
S1,,,,,,,,,,,,,H1,container_terminal
S2,2000,L1,ROAD,truck_40t,100,,,,,diesel,30,8000,,
S3,1000,L1,road,truck_40t,abc,,,,,,,,,
S3,1000,L2,road,truck_40t,10,,,,,,,,,
`

func TestRead(t *testing.T) {
	items, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d shipments, want 3", len(items))
	}

	s1 := items[0]
	if s1.Err != nil || s1.Shipment.WeightKg != 10000 || len(s1.Shipment.Legs) != 2 || len(s1.Shipment.Hubs) != 1 {
		t.Errorf("S1 = %+v", s1)
	}
	if l := s1.Shipment.Legs[1]; l.DistanceKm != 0 || l.From.Lat != 53.55 || l.To.Lon != 4.48 {
		t.Errorf("S1 L2 = %+v", l)
	}

	s2 := items[1].Shipment
	if items[1].Err != nil || s2.Legs[0].Mode != calc.Road || s2.Legs[0].Fuel == nil ||
		s2.Legs[0].Fuel.Amount != 30 || s2.Legs[0].Fuel.VehicleTonneKm != 8000 {
		t.Errorf("S2 = %+v", items[1])
	}

	if items[2].Err == nil || !strings.Contains(items[2].Err.Error(), "line 6") {
		t.Errorf("S3 should fail on line 6, got %v", items[2].Err)
	}
}

func TestRead_Structural(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"missing weight": "shipment_id,mode\nS1,road\n",
		"no id":          "weight_kg,mode\n1,road\n",
		"empty id":       "shipment_id,weight_kg\n,100\n",
	}
	for name, in := range cases {
		if _, err := Read(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestRead_ConflictingWeight(t *testing.T) {
	items, err := Read(strings.NewReader("shipment_id,weight_kg,mode\nS1,100,road\nS1,200,road\n"))
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Err == nil {
		t.Error("want conflicting weight error")
	}
}

func TestWriter(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Write("S1", calc.Result{WeightKg: 10000, TonneKm: 5000, TTWKg: 300, WTTKg: 75, WTWKg: 375, IntensityG: 75,
		PrimaryDataShare: 0.5, Dataset: "d"}, nil)
	_ = w.Write("S2", calc.Result{}, errors.New("boom"))
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "shipment_id,weight_kg,tonne_km,ttw_kg,wtt_kg,wtw_kg,intensity_g_per_tkm,primary_data_share,dataset,error\n" +
		"S1,10000.0000,5000.0000,300.0000,75.0000,375.0000,75.0000,0.5000,d,\n" +
		"S2,,,,,,,,,boom\n"
	if buf.String() != want {
		t.Errorf("got\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestRead_BOMHeader(t *testing.T) {
	items, err := Read(strings.NewReader("\uFEFFshipment_id,weight_kg,mode,distance_km\nS1,100,road,5\n"))
	if err != nil || len(items) != 1 || items[0].Err != nil {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestRead_TEU(t *testing.T) {
	items, err := Read(strings.NewReader("shipment_id,teu,container_load,mode,vehicle_type,distance_km\nC1,2,average,sea,container_ship,100\nC1,,,road,hgv,10\nC2,1,heavy,sea,x,1\nC2,1,light,sea,x,1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s := items[0].Shipment; items[0].Err != nil || s.TEU != 2 || s.ContainerLoad != "average" || len(s.Legs) != 2 {
		t.Errorf("C1 = %+v", items[0])
	}
	if items[1].Err == nil {
		t.Error("C2: want conflicting container_load error")
	}
}

func TestRead_LoadFactor(t *testing.T) {
	items, err := Read(strings.NewReader("shipment_id,weight_kg,mode,vehicle_type,distance_km,load_factor,empty_trip_factor\nS,1000,road,hgv,10,0.8,0.25\n"))
	if err != nil {
		t.Fatal(err)
	}
	if l := items[0].Shipment.Legs[0]; items[0].Err != nil || l.LoadFactor != 0.8 || l.EmptyTripFactor != 0.25 {
		t.Errorf("leg = %+v, %v", l, items[0].Err)
	}
}

func TestRead_Energy(t *testing.T) {
	items, err := Read(strings.NewReader("shipment_id,weight_kg,mode,vehicle_type,distance_km,energy_kwh_per_tkm,country\nS,1000,rail,electric,10,0.03,DNK\n"))
	if err != nil {
		t.Fatal(err)
	}
	if l := items[0].Shipment.Legs[0]; items[0].Err != nil || l.EnergyKWhPerTkm != 0.03 || l.Country != "DNK" {
		t.Errorf("leg = %+v, %v", l, items[0].Err)
	}
}
