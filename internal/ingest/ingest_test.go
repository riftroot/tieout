package ingest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	okField  = "work_order_id,asset_id,date,quantity,unit\nW1,A,2026-10-01,10,m3\n"
	okTel    = "asset_id,date,quantity,unit\nA,2026-10-01,10,m3\n"
	okLedger = "invoice_line_id,work_order_id,asset_id,date,quantity,unit,unit_price_usd\nI1,W1,A,2026-10-01,10,m3,1.25\n"
)

func TestLoadRejectsBadInput(t *testing.T) {
	const ledgerHead = "invoice_line_id,work_order_id,asset_id,date,quantity,unit,unit_price_usd\n"
	cases := []struct {
		name, file, body string
		wantLine         int
		wantMsg          string
	}{
		{"bad date", "field", "work_order_id,asset_id,date,quantity,unit\nW1,A,2026-02-30,1,m3\n", 2, "date"},
		{"negative quantity", "telemetry", "asset_id,date,quantity,unit\nA,2026-10-01,5,m3\nA,2026-10-01,-5,m3\n", 3, "quantity"},
		{"too many decimals", "telemetry", "asset_id,date,quantity,unit\nA,2026-10-01,1.0001,m3\n", 2, "quantity"},
		{"empty unit", "telemetry", "asset_id,date,quantity,unit\nA,2026-10-01,1,\n", 2, "unit"},
		{"short row", "field", "work_order_id,asset_id,date,quantity,unit\nW1,A,2026-10-01,1\n", 2, "expected 5 fields, got 4"},
		{"wrong header", "telemetry", "asset,date,quantity,unit\n", 1, "header"},
		{"control character in asset id", "telemetry", "asset_id,date,quantity,unit\nA\x1b[2J,2026-10-01,1,m3\n", 2, "control or invisible"},
		{"control character in optional work order", "ledger", ledgerHead + "I1,W\x07,A,2026-10-01,1,m3,1.25\n", 2, "control or invisible"},
		{"bidi override in asset id", "telemetry", "asset_id,date,quantity,unit\nA\u202eB,2026-10-01,1,m3\n", 2, "formatting characters"},
		{"zero-width space in unit", "telemetry", "asset_id,date,quantity,unit\nA,2026-10-01,1,m\u200b3\n", 2, "formatting characters"},
		{"bad price", "ledger", ledgerHead + "I1,,A,2026-10-01,1,m3,abc\n", 2, "unit_price_usd"},
		{"duplicate invoice line", "ledger", ledgerHead + "I1,,A,2026-10-01,1,m3,1\nI1,,A,2026-10-02,1,m3,1\n", 3, "already used on line 2"},
		{"duplicate work order", "field", "work_order_id,asset_id,date,quantity,unit\nW1,A,2026-10-01,1,m3\nW1,B,2026-10-01,1,m3\n", 3, "already used on line 2"},
		{"unterminated quote", "field", "work_order_id,asset_id,date,quantity,unit\nW1,\"A,2026-10-01,1,m3\n", 2, "quote"},
		{"empty file", "field", "", 0, "empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := map[string]string{"field": okField, "telemetry": okTel, "ledger": okLedger}
			paths[c.file] = c.body
			var p [3]string
			for i, n := range []string{"field", "telemetry", "ledger"} {
				p[i] = filepath.Join(dir, n+".csv")
				if err := os.WriteFile(p[i], []byte(paths[n]), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Load(p[0], p[1], p[2])
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("err = %v; want *Error", err)
			}
			if e.File != filepath.Join(dir, c.file+".csv") || e.Line != c.wantLine || !strings.Contains(e.Msg, c.wantMsg) {
				t.Errorf("got %q (line %d); want line %d containing %q", e.Error(), e.Line, c.wantLine, c.wantMsg)
			}
		})
	}
}
