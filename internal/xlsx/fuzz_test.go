package xlsx

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"
)

// buildXLSX wraps sheet and shared-strings XML in a minimal valid workbook.
func buildXLSX(sheet, shared string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	add("xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="S" r:id="rId1"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`)
	add("xl/worksheets/sheet1.xml", sheet)
	if shared != "" {
		add("xl/sharedStrings.xml", shared)
	}
	_ = zw.Close()
	return buf.Bytes()
}

// Any sheet XML must give rows or an error, never a panic or a huge allocation.
func FuzzRows(f *testing.F) {
	f.Add(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><v>1.5</v></c></row></sheetData></worksheet>`, `<sst><si><t>a</t></si></sst>`)
	f.Add(`<worksheet><sheetData><row r="3"><c r="AB3" t="inlineStr"><is><t>x</t></is></c></row></sheetData></worksheet>`, ``)
	f.Add(`<worksheet><sheetData><row><c><v>1</v></c></row></sheetData></worksheet>`, ``)
	f.Fuzz(func(t *testing.T, sheet, shared string) {
		b := buildXLSX(sheet, shared)
		file, err := Open(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return
		}
		rows, err := file.Rows("S")
		if err != nil {
			return
		}
		if len(rows) > maxRows {
			t.Fatalf("%d rows", len(rows))
		}
		for _, r := range rows {
			if len(r) > maxCols {
				t.Fatalf("%d columns", len(r))
			}
		}
	})
}

// Arbitrary bytes, seeded with real workbooks.
func FuzzOpen(f *testing.F) {
	for _, name := range []string{"testdata/sample.xlsx", "testdata/sample_shared.xlsx"} {
		b, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		file, err := Open(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return
		}
		for _, s := range file.SheetNames() {
			_, _ = file.Rows(s)
		}
	})
}
