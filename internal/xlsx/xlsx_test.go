package xlsx

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Fixtures:
//
//	sample.xlsx         written by openpyxl: inline strings, absolute part paths
//	sample_shared.xlsx  same workbook re-saved by LibreOffice: shared strings,
//	                    relative part paths, a formula with a cached value
func openFixture(t *testing.T, name string) *File {
	t.Helper()
	fh, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fh.Close() })
	st, err := fh.Stat()
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(fh, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRows(t *testing.T) {
	far := make([]string, 28)
	far[27] = "far"
	want := [][]string{
		{"name", "value", "flag"},
		{"alpha", "1.5", "TRUE"},
		{"rich text", "0.000123456789"},
		nil,
		far,
	}
	for _, name := range []string{"sample.xlsx", "sample_shared.xlsx"} {
		t.Run(name, func(t *testing.T) {
			f := openFixture(t, name)
			if got := f.SheetNames(); !reflect.DeepEqual(got, []string{"First", "Second"}) {
				t.Fatalf("sheets = %v", got)
			}
			rows, err := f.Rows("First")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(rows, want) {
				t.Errorf("rows =\n%q\nwant\n%q", rows, want)
			}
			rows, err = f.Rows("Second")
			if err != nil || len(rows) != 1 || rows[0][0] != "only" {
				t.Errorf("Second = %q, %v", rows, err)
			}
			if _, err := f.Rows("Nope"); err == nil {
				t.Error("want error for missing sheet")
			}
		})
	}
}

func TestColumnIndex(t *testing.T) {
	for ref, want := range map[string]int{"A1": 0, "Z9": 25, "AA1": 26, "AB5": 27, "XFD1048576": 16383} {
		if got, err := columnIndex(ref); err != nil || got != want {
			t.Errorf("%s = %d, %v; want %d", ref, got, err, want)
		}
	}
	for _, bad := range []string{"", "12", "AB"} {
		if _, err := columnIndex(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestOpen_NotZip(t *testing.T) {
	fh, err := os.Open("xlsx.go")
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	st, _ := fh.Stat()
	if _, err := Open(fh, st.Size()); err == nil {
		t.Error("want error for non-zip input")
	}
}

func TestRows_Hostile(t *testing.T) {
	for name, sheet := range map[string]string{
		"negative row":   `<worksheet><sheetData><row r="-3"><c><v>1</v></c></row></sheetData></worksheet>`,
		"huge row":       `<worksheet><sheetData><row r="50000000"><c><v>1</v></c></row></sheetData></worksheet>`,
		"overflow col":   `<worksheet><sheetData><row r="1"><c r="AAAAAAAAAAAAAAAAAA1"><v>1</v></c></row></sheetData></worksheet>`,
		"col past XFD":   `<worksheet><sheetData><row r="1"><c r="XFE1"><v>1</v></c></row></sheetData></worksheet>`,
		"too many cells": `<worksheet><sheetData>` + strings.Repeat(`<row><c r="XFD1"><v>1</v></c></row>`, 300) + `</sheetData></worksheet>`,
	} {
		b := buildXLSX(sheet, "")
		f, err := Open(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Rows("S"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	// The last valid column still works.
	b := buildXLSX(`<worksheet><sheetData><row r="1"><c r="XFD1"><v>1</v></c></row></sheetData></worksheet>`, "")
	f, _ := Open(bytes.NewReader(b), int64(len(b)))
	if rows, err := f.Rows("S"); err != nil || len(rows[0]) != maxCols {
		t.Errorf("XFD: %v", err)
	}
}
