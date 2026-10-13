// Package xlsx is a minimal, read-only .xlsx reader built on the standard
// library. It returns cell values as strings and supports what tabular data
// files use: shared strings, inline strings, numbers and booleans. Formulas
// are read as their cached values. Styles and dates are not interpreted.
package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// Excel's own limits; anything beyond is malformed or hostile.
const (
	maxRows = 1 << 20 // 1,048,576
	maxCols = 1 << 14 // 16,384 (column XFD)
	// maxCells caps cells allocated per sheet (including empty ones before
	// a referenced cell), so a small hostile file can't demand gigabytes.
	maxCells = 1 << 22
)

// maxPartBytes caps how much of any one XML part is read (zip-bomb guard).
const maxPartBytes = 512 << 20

// File is an opened workbook.
type File struct {
	zr      *zip.Reader
	sheets  []sheetRef
	strings []string
}

type sheetRef struct {
	name, target string
}

// Open reads the workbook structure from r.
func Open(r io.ReaderAt, size int64) (*File, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("xlsx: not a zip file: %w", err)
	}
	f := &File{zr: zr}
	if err := f.readWorkbook(); err != nil {
		return nil, err
	}
	if err := f.readSharedStrings(); err != nil {
		return nil, err
	}
	return f, nil
}

// SheetNames returns the sheet names in workbook order.
func (f *File) SheetNames() []string {
	names := make([]string, len(f.sheets))
	for i, s := range f.sheets {
		names[i] = s.name
	}
	return names
}

// Rows returns all rows of the named sheet. Each row is as long as its last
// non-empty cell; missing cells are empty strings. Empty rows are kept so row
// indexes match the spreadsheet (row 1 is index 0).
func (f *File) Rows(sheet string) ([][]string, error) {
	var target string
	for _, s := range f.sheets {
		if s.name == sheet {
			target = s.target
		}
	}
	if target == "" {
		return nil, fmt.Errorf("xlsx: no sheet %q", sheet)
	}
	rc, err := f.open(target)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var ws struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				Ref    string `xml:"r,attr"`
				Type   string `xml:"t,attr"`
				Value  string `xml:"v"`
				Inline struct {
					Text string `xml:"t"`
					Runs []struct {
						Text string `xml:"t"`
					} `xml:"r"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.NewDecoder(rc).Decode(&ws); err != nil {
		return nil, fmt.Errorf("xlsx: sheet %q: %w", sheet, err)
	}

	var rows [][]string
	cells := 0
	for _, xr := range ws.Rows {
		idx := xr.R - 1
		if xr.R == 0 { // r is optional; fall back to sequence
			idx = len(rows)
		}
		if idx < 0 || idx >= maxRows {
			return nil, fmt.Errorf("xlsx: sheet %q: row number %d out of range", sheet, xr.R)
		}
		for len(rows) <= idx {
			rows = append(rows, nil)
		}
		var row []string
		for i, c := range xr.Cells {
			col := i
			if c.Ref != "" {
				if col, err = columnIndex(c.Ref); err != nil {
					return nil, err
				}
			}
			var v string
			switch c.Type {
			case "s":
				var n int
				if _, err := fmt.Sscan(c.Value, &n); err != nil || n < 0 || n >= len(f.strings) {
					return nil, fmt.Errorf("xlsx: cell %s: bad shared string index %q", c.Ref, c.Value)
				}
				v = f.strings[n]
			case "inlineStr":
				v = c.Inline.Text
				for _, r := range c.Inline.Runs {
					v += r.Text
				}
			case "b":
				v = map[string]string{"1": "TRUE", "0": "FALSE"}[c.Value]
			default: // "n", "str", "e" and untyped
				v = c.Value
			}
			if col >= len(row) {
				cells += col + 1 - len(row)
				if cells > maxCells {
					return nil, fmt.Errorf("xlsx: sheet %q: more than %d cells", sheet, maxCells)
				}
				row = append(row, make([]string, col+1-len(row))...)
			}
			row[col] = v
		}
		rows[idx] = row
	}
	return rows, nil
}

// columnIndex converts a cell reference like "AB12" to a 0-based column.
func columnIndex(ref string) (int, error) {
	n := 0
	for i, r := range ref {
		if r >= 'A' && r <= 'Z' {
			n = n*26 + int(r-'A'+1)
			if n > maxCols {
				return 0, fmt.Errorf("xlsx: column in %q beyond %d", ref, maxCols)
			}
			continue
		}
		if i == 0 {
			break
		}
		return n - 1, nil
	}
	return 0, fmt.Errorf("xlsx: bad cell reference %q", ref)
}

func (f *File) readWorkbook() error {
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := f.decode("xl/workbook.xml", &wb); err != nil {
		return err
	}
	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := f.decode("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return err
	}
	targets := map[string]string{}
	for _, r := range rels.Rels {
		t := r.Target
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Join("xl", t)
		}
		targets[r.ID] = t
	}
	for _, s := range wb.Sheets {
		t, ok := targets[s.RID]
		if !ok {
			return fmt.Errorf("xlsx: sheet %q has no relationship target", s.Name)
		}
		f.sheets = append(f.sheets, sheetRef{name: s.Name, target: t})
	}
	if len(f.sheets) == 0 {
		return errors.New("xlsx: workbook has no sheets")
	}
	return nil
}

func (f *File) readSharedStrings() error {
	var sst struct {
		Items []struct {
			Text string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		} `xml:"si"`
	}
	err := f.decode("xl/sharedStrings.xml", &sst)
	if errors.Is(err, errNotFound) {
		return nil // workbooks with no strings have no shared strings part
	}
	if err != nil {
		return err
	}
	f.strings = make([]string, len(sst.Items))
	for i, it := range sst.Items {
		s := it.Text
		for _, r := range it.Runs {
			s += r.Text
		}
		f.strings[i] = s
	}
	return nil
}

var errNotFound = errors.New("xlsx: part not found")

func (f *File) open(name string) (io.ReadCloser, error) {
	for _, zf := range f.zr.File {
		if zf.Name == name {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			return struct {
				io.Reader
				io.Closer
			}{io.LimitReader(rc, maxPartBytes), rc}, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", errNotFound, name)
}

func (f *File) decode(name string, v any) error {
	rc, err := f.open(name)
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := xml.NewDecoder(rc).Decode(v); err != nil {
		return fmt.Errorf("xlsx: %s: %w", name, err)
	}
	return nil
}
