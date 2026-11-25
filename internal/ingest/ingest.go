// Package ingest loads and validates the three input CSVs. Every rejection is
// an *Error carrying the file and line of the offending row.
package ingest

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/riftroot/tieout/internal/decimal"
)

// Scales and bounds chosen so quantity x price fits in an int64 with room to spare.
const (
	QtyScale      = 3
	PriceScale    = 4
	maxQtyDigits  = 6
	maxPriceDigit = 5
)

type FieldRow struct {
	Line        int
	WorkOrderID string
	AssetID     string
	Date        string
	Quantity    int64 // units x 10^QtyScale
	Unit        string
}

type TelemetryRow struct {
	Line     int
	AssetID  string
	Date     string
	Quantity int64
	Unit     string
}

type LedgerRow struct {
	Line          int
	InvoiceLineID string
	WorkOrderID   string // may be empty
	AssetID       string
	Date          string
	Quantity      int64
	Unit          string
	UnitPrice     int64 // USD x 10^PriceScale
}

type Data struct {
	Field     []FieldRow
	Telemetry []TelemetryRow
	Ledger    []LedgerRow
}

type Error struct {
	File string
	Line int // 0 for file-level problems
	Msg  string
}

func (e *Error) Error() string {
	if e.Line == 0 {
		return fmt.Sprintf("%s: %s", e.File, e.Msg)
	}
	return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
}

// Load reads all three files and returns the first validation error found.
func Load(fieldPath, telemetryPath, ledgerPath string) (Data, error) {
	var d Data
	var err error
	if d.Field, err = loadField(fieldPath); err != nil {
		return Data{}, err
	}
	if d.Telemetry, err = loadTelemetry(telemetryPath); err != nil {
		return Data{}, err
	}
	if d.Ledger, err = loadLedger(ledgerPath); err != nil {
		return Data{}, err
	}
	return d, nil
}

type record struct {
	line   int
	fields []string
}

func readRecords(path string, header []string) ([]record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &Error{File: path, Msg: err.Error()}
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	var out []record
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				return nil, &Error{File: path, Line: pe.StartLine, Msg: pe.Err.Error()}
			}
			return nil, &Error{File: path, Msg: err.Error()}
		}
		line, _ := r.FieldPos(0)
		for i := range rec {
			rec[i] = strings.TrimSpace(rec[i])
		}
		out = append(out, record{line: line, fields: rec})
	}
	if len(out) == 0 {
		return nil, &Error{File: path, Msg: "file is empty; expected header " + strings.Join(header, ",")}
	}
	if got := strings.Join(out[0].fields, ","); got != strings.Join(header, ",") {
		return nil, &Error{File: path, Line: out[0].line, Msg: fmt.Sprintf("header is %q; expected %q", got, strings.Join(header, ","))}
	}
	rows := out[1:]
	for _, rec := range rows {
		if len(rec.fields) != len(header) {
			return nil, &Error{File: path, Line: rec.line, Msg: fmt.Sprintf("expected %d fields, got %d", len(header), len(rec.fields))}
		}
	}
	return rows, nil
}

type rowParser struct {
	file string
	rec  record
	err  *Error
}

func (p *rowParser) fail(col, msg string) {
	if p.err == nil {
		p.err = &Error{File: p.file, Line: p.rec.line, Msg: col + ": " + msg}
	}
}

func (p *rowParser) text(i int, col string) string {
	v := p.rec.fields[i]
	if v == "" {
		p.fail(col, "must not be empty")
	}
	return p.optText(i, col)
}

// optText is a column that may be empty but, like every text column, reaches the report
// verbatim, so control characters are refused.
func (p *rowParser) optText(i int, col string) string {
	v := p.rec.fields[i]
	if strings.IndexFunc(v, func(r rune) bool { return unicode.IsControl(r) || r == utf8.RuneError }) >= 0 {
		p.fail(col, "must not contain control characters or invalid UTF-8")
	}
	return v
}

func (p *rowParser) date(i int) string {
	v := p.rec.fields[i]
	if t, err := time.Parse(time.DateOnly, v); err != nil || t.Format(time.DateOnly) != v {
		p.fail("date", fmt.Sprintf("%q is not a YYYY-MM-DD date", v))
	}
	return v
}

func (p *rowParser) qty(i int) int64 {
	v, err := decimal.Parse(p.rec.fields[i], QtyScale, maxQtyDigits)
	if err != nil {
		p.fail("quantity", err.Error())
	}
	return v
}

func (p *rowParser) price(i int) int64 {
	v, err := decimal.Parse(p.rec.fields[i], PriceScale, maxPriceDigit)
	if err != nil {
		p.fail("unit_price_usd", err.Error())
	}
	return v
}

func loadField(path string) ([]FieldRow, error) {
	recs, err := readRecords(path, []string{"work_order_id", "asset_id", "date", "quantity", "unit"})
	if err != nil {
		return nil, err
	}
	seen := map[string]int{}
	var rows []FieldRow
	for _, rec := range recs {
		p := &rowParser{file: path, rec: rec}
		row := FieldRow{Line: rec.line, WorkOrderID: p.text(0, "work_order_id"), AssetID: p.text(1, "asset_id"),
			Date: p.date(2), Quantity: p.qty(3), Unit: p.text(4, "unit")}
		if p.err != nil {
			return nil, p.err
		}
		if first, dup := seen[row.WorkOrderID]; dup {
			return nil, &Error{File: path, Line: rec.line, Msg: fmt.Sprintf("work_order_id %q already used on line %d", row.WorkOrderID, first)}
		}
		seen[row.WorkOrderID] = rec.line
		rows = append(rows, row)
	}
	return rows, nil
}

func loadTelemetry(path string) ([]TelemetryRow, error) {
	recs, err := readRecords(path, []string{"asset_id", "date", "quantity", "unit"})
	if err != nil {
		return nil, err
	}
	var rows []TelemetryRow
	for _, rec := range recs {
		p := &rowParser{file: path, rec: rec}
		row := TelemetryRow{Line: rec.line, AssetID: p.text(0, "asset_id"), Date: p.date(1), Quantity: p.qty(2), Unit: p.text(3, "unit")}
		if p.err != nil {
			return nil, p.err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func loadLedger(path string) ([]LedgerRow, error) {
	recs, err := readRecords(path, []string{"invoice_line_id", "work_order_id", "asset_id", "date", "quantity", "unit", "unit_price_usd"})
	if err != nil {
		return nil, err
	}
	seen := map[string]int{}
	var rows []LedgerRow
	for _, rec := range recs {
		p := &rowParser{file: path, rec: rec}
		row := LedgerRow{Line: rec.line, InvoiceLineID: p.text(0, "invoice_line_id"), WorkOrderID: p.optText(1, "work_order_id"),
			AssetID: p.text(2, "asset_id"), Date: p.date(3), Quantity: p.qty(4), Unit: p.text(5, "unit"), UnitPrice: p.price(6)}
		if p.err != nil {
			return nil, p.err
		}
		if first, dup := seen[row.InvoiceLineID]; dup {
			return nil, &Error{File: path, Line: rec.line, Msg: fmt.Sprintf("invoice_line_id %q already used on line %d", row.InvoiceLineID, first)}
		}
		seen[row.InvoiceLineID] = rec.line
		rows = append(rows, row)
	}
	return rows, nil
}
