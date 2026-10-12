package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/majidkabir/carbonara/internal/desnz"
	"github.com/majidkabir/carbonara/internal/xlsx"
)

// runImportDESNZ converts the UK DESNZ flat-file spreadsheet to a dataset.
func runImportDESNZ(args []string) error {
	fs := flag.NewFlagSet("import-desnz", flag.ExitOnError)
	out := fs.String("o", "-", "output JSON file (default stdout)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("import-desnz: expected the DESNZ flat-file .xlsx")
	}

	fh, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return err
	}
	book, err := xlsx.Open(fh, st.Size())
	if err != nil {
		return err
	}

	// The factor sheet name has varied between years, so try each sheet.
	var res desnz.Result
	var lastErr error
	found := false
	for _, name := range book.SheetNames() {
		rows, err := book.Rows(name)
		if err != nil {
			return err
		}
		if res, lastErr = desnz.Parse(rows); lastErr == nil {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("import-desnz: no sheet with flat-file factors: %w", lastErr)
	}

	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	fmt.Fprintf(os.Stderr, "%s: %d leg factors, %d fuel factors, %d blank rows skipped, %d warnings\n",
		res.Dataset.Name, len(res.Dataset.Legs), len(res.Dataset.Fuels), res.Blank, len(res.Warnings))

	w := os.Stdout
	if *out != "-" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res.Dataset)
}
