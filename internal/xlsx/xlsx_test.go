package xlsx

import (
	"os"
	"reflect"
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
