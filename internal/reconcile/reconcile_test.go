package reconcile

import (
	"testing"

	"github.com/riftroot/tieout/internal/ingest"
)

func f(wo, asset, date string, q int64, unit string) ingest.FieldRow {
	return ingest.FieldRow{WorkOrderID: wo, AssetID: asset, Date: date, Quantity: q, Unit: unit}
}
func t(asset, date string, q int64, unit string) ingest.TelemetryRow {
	return ingest.TelemetryRow{AssetID: asset, Date: date, Quantity: q, Unit: unit}
}
func l(id, asset, date string, q int64, unit string, price int64) ingest.LedgerRow {
	return ingest.LedgerRow{InvoiceLineID: id, AssetID: asset, Date: date, Quantity: q, Unit: unit, UnitPrice: price}
}

func ptr(v int64) *int64 { return &v }

func TestScan(t_ *testing.T) {
	const d1, d2 = "2026-10-01", "2026-10-02"
	opt := Options{ToleranceMilliPct: 2000}
	cases := []struct {
		name   string
		data   ingest.Data
		opt    Options
		kind   Kind   // expected single finding kind; "" means none
		amount *int64 // expected cents; nil means unpriced
	}{
		{"clean", ingest.Data{
			Field:     []ingest.FieldRow{f("W1", "A", d1, 10000, "m3")},
			Telemetry: []ingest.TelemetryRow{t("A", d1, 10000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 10000, "m3", 12500)},
		}, opt, "", nil},
		{"unbilled priced at latest earlier price", ingest.Data{
			Field: []ingest.FieldRow{f("W2", "A", d2, 10000, "m3")},
			Telemetry: []ingest.TelemetryRow{t("A", d2, 10000, "m3"), t("A", "2026-09-01", 1000, "m3"),
				t("A", d1, 1000, "m3"), t("A", "2026-10-09", 1000, "m3")},
			Ledger: []ingest.LedgerRow{l("I0", "A", "2026-09-01", 1000, "m3", 10000),
				l("I1", "A", d1, 1000, "m3", 12500), l("I9", "A", "2026-10-09", 1000, "m3", 99999)},
		}, opt, Unbilled, ptr(1250)},
		{"unbilled with no prior price is unpriced", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 10000, "m3"), t("A", d2, 1000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I9", "A", d2, 1000, "m3", 12500)},
		}, opt, Unbilled, nil},
		{"unbilled never priced from another unit", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d2, 10000, "m3"), t("A", d1, 1000, "gal")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 1000, "gal", 12500)},
		}, opt, Unbilled, nil},
		{"quantity mismatch under-billed", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 10000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 8000, "m3", 12500)},
		}, opt, QuantityMismatch, ptr(250)},
		{"quantity mismatch over-billed is negative", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 8000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 10000, "m3", 12500)},
		}, opt, QuantityMismatch, ptr(-250)},
		{"multiple telemetry readings are summed", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 6000, "m3"), t("A", d1, 4000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 10000, "m3", 12500)},
		}, opt, "", nil},
		{"difference at the tolerance boundary passes", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 100000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 98000, "m3", 10000)},
		}, opt, "", nil},
		{"one milli-unit beyond tolerance flags", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 100000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 97999, "m3", 10000)},
		}, opt, QuantityMismatch, ptr(200)},
		{"tolerance option widens the band", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 100000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 95000, "m3", 10000)},
		}, Options{ToleranceMilliPct: 5000}, "", nil},
		{"rounding is half up to the cent", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 1000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 500, "m3", 100)},
		}, opt, QuantityMismatch, ptr(1)},
		{"unsupported billing", ingest.Data{
			Ledger: []ingest.LedgerRow{l("I1", "A", d1, 4000, "m3", 12500), l("I2", "A", d1, 1000, "m3", 12500)},
		}, opt, UnsupportedBilling, ptr(-625)},
		{"field and telemetry disagree", ingest.Data{
			Field:     []ingest.FieldRow{f("W1", "A", d1, 12000, "m3")},
			Telemetry: []ingest.TelemetryRow{t("A", d1, 10000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 10000, "m3", 12500)},
		}, opt, FieldTelemetryMismatch, nil},
		{"unit mismatch is never coerced", ingest.Data{
			Field:     []ingest.FieldRow{f("W1", "A", d1, 10000, "m3")},
			Telemetry: []ingest.TelemetryRow{t("A", d1, 10000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 10000, "gal", 12500)},
		}, opt, UnitMismatch, nil},
		{"over-billing against the field record when telemetry is missing", ingest.Data{
			Field:  []ingest.FieldRow{f("W1", "A", d1, 8000, "m3")},
			Ledger: []ingest.LedgerRow{l("I1", "A", d1, 10000, "m3", 12500)},
		}, opt, QuantityMismatch, ptr(-250)},
		{"mixed ledger prices priced at the highest", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 10000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I1", "A", d1, 4000, "m3", 50000), l("I2", "A", d1, 4000, "m3", 12500)},
		}, opt, QuantityMismatch, ptr(1000)},
		{"mixed ledger prices, lines reversed, same amount", ingest.Data{
			Telemetry: []ingest.TelemetryRow{t("A", d1, 10000, "m3")},
			Ledger:    []ingest.LedgerRow{l("I2", "A", d1, 4000, "m3", 12500), l("I1", "A", d1, 4000, "m3", 50000)},
		}, opt, QuantityMismatch, ptr(1000)},
		{"zero-quantity work is not unbilled", ingest.Data{
			Field: []ingest.FieldRow{f("W1", "A", d1, 0, "m3")},
		}, opt, "", nil},
	}
	for _, c := range cases {
		t_.Run(c.name, func(t_ *testing.T) {
			got, err := Scan(c.data, c.opt)
			if err != nil {
				t_.Fatal(err)
			}
			if c.kind == "" {
				if len(got) != 0 {
					t_.Fatalf("want no findings, got %+v", got)
				}
				return
			}
			if len(got) != 1 {
				t_.Fatalf("want 1 finding, got %d: %+v", len(got), got)
			}
			g := got[0]
			if g.Kind != c.kind {
				t_.Errorf("kind = %s, want %s", g.Kind, c.kind)
			}
			switch {
			case c.amount == nil && g.AmountCents != nil:
				t_.Errorf("amount = %d, want unpriced", *g.AmountCents)
			case c.amount != nil && g.AmountCents == nil:
				t_.Errorf("unpriced, want %d cents", *c.amount)
			case c.amount != nil && *g.AmountCents != *c.amount:
				t_.Errorf("amount = %d cents, want %d", *g.AmountCents, *c.amount)
			}
		})
	}
}

func TestSortedByAbsoluteValue(t_ *testing.T) {
	data := ingest.Data{
		Telemetry: []ingest.TelemetryRow{t("A", "2026-10-01", 10000, "m3"), t("B", "2026-10-01", 1000, "m3"), t("C", "2026-10-01", 5000, "m3")},
		Ledger: []ingest.LedgerRow{l("I1", "A", "2026-10-01", 5000, "m3", 10000), l("I2", "B", "2026-10-01", 9000, "m3", 10000),
			l("I3", "C", "2026-10-01", 4000, "m3", 10000), l("I4", "Z", "2026-10-01", 1000, "m3", 10000)},
	}
	got, err := Scan(data, Options{ToleranceMilliPct: 2000})
	if err != nil {
		t_.Fatal(err)
	}
	var order []string
	for _, g := range got {
		order = append(order, g.AssetID)
	}
	want := []string{"B", "A", "C", "Z"} // 80.00, 50.00, 10.00, 10.00 (Z ties C, later asset id)
	if len(order) != len(want) {
		t_.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t_.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestOverflowFailsInsteadOfWrapping(t_ *testing.T) {
	const big = 999_999_000 // the largest quantity ingest admits (999999 units)
	var tel []ingest.TelemetryRow
	for range 10_000 {
		tel = append(tel, t("A", "2026-10-01", big, "m3"))
	}
	data := ingest.Data{Telemetry: tel, Ledger: []ingest.LedgerRow{l("I1", "A", "2026-10-01", 1000, "m3", 999_990_000)}}
	if _, err := Scan(data, Options{ToleranceMilliPct: 2000}); err == nil {
		t_.Fatal("want an overflow error, got none")
	}
}
