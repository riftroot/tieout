// Package reconcile compares field, telemetry and ledger records per
// (asset, date) and prices each disagreement in integer cents.
package reconcile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riftroot/tieout/internal/decimal"
	"github.com/riftroot/tieout/internal/ingest"
)

type Kind string

const (
	Unbilled               Kind = "unbilled"
	QuantityMismatch       Kind = "quantity_mismatch"
	UnsupportedBilling     Kind = "unsupported_billing"
	FieldTelemetryMismatch Kind = "field_telemetry_mismatch"
	UnitMismatch           Kind = "unit_mismatch"
)

// Kinds lists every finding kind in report order.
var Kinds = []Kind{Unbilled, QuantityMismatch, UnsupportedBilling, FieldTelemetryMismatch, UnitMismatch}

// Options configures a scan. ToleranceMilliPct is the allowed relative
// difference in thousandths of a percent (2% = 2000).
type Options struct {
	ToleranceMilliPct int64
}

// Finding is one disagreement. Quantities are scaled by 10^ingest.QtyScale and
// nil when that source has no record. AmountCents is nil when the finding is
// unpriced; otherwise positive means work not billed and negative means
// billing without matching work.
type Finding struct {
	Kind        Kind
	AssetID     string
	Date        string
	Unit        string
	Field       *int64
	Telemetry   *int64
	Ledger      *int64
	AmountCents *int64
	Detail      string
}

type key struct{ asset, date string }

type bucket struct {
	field     []ingest.FieldRow
	telemetry []ingest.TelemetryRow
	ledger    []ingest.LedgerRow
}

// Scan returns findings sorted by absolute dollar value, largest first.
func Scan(d ingest.Data, opt Options) []Finding {
	buckets := map[key]*bucket{}
	get := func(asset, date string) *bucket {
		k := key{asset, date}
		if buckets[k] == nil {
			buckets[k] = &bucket{}
		}
		return buckets[k]
	}
	for _, r := range d.Field {
		b := get(r.AssetID, r.Date)
		b.field = append(b.field, r)
	}
	for _, r := range d.Telemetry {
		b := get(r.AssetID, r.Date)
		b.telemetry = append(b.telemetry, r)
	}
	for _, r := range d.Ledger {
		b := get(r.AssetID, r.Date)
		b.ledger = append(b.ledger, r)
	}

	prices := map[string][]ingest.LedgerRow{}
	for _, r := range d.Ledger {
		prices[r.AssetID] = append(prices[r.AssetID], r)
	}

	var out []Finding
	for k, b := range buckets {
		out = append(out, scanBucket(k, b, prices[k.asset], opt)...)
	}
	sortFindings(out)
	return out
}

func scanBucket(k key, b *bucket, assetLedger []ingest.LedgerRow, opt Options) []Finding {
	var fieldQ, telQ, ledQ int64
	units := map[string][]string{}
	note := func(source, unit string) {
		if !slices.Contains(units[unit], source) {
			units[unit] = append(units[unit], source)
		}
	}
	for _, r := range b.field {
		fieldQ += r.Quantity
		note("field", r.Unit)
	}
	for _, r := range b.telemetry {
		telQ += r.Quantity
		note("telemetry", r.Unit)
	}
	for _, r := range b.ledger {
		ledQ += r.Quantity
		note("ledger", r.Unit)
	}
	hasF, hasT, hasL := len(b.field) > 0, len(b.telemetry) > 0, len(b.ledger) > 0

	base := Finding{AssetID: k.asset, Date: k.date}
	if hasF {
		base.Field = &fieldQ
	}
	if hasT {
		base.Telemetry = &telQ
	}
	if hasL {
		base.Ledger = &ledQ
	}

	if len(units) > 1 {
		f := base
		f.Kind = UnitMismatch
		f.Field, f.Telemetry, f.Ledger = nil, nil, nil
		names := make([]string, 0, len(units))
		for u := range units {
			names = append(names, u)
		}
		slices.Sort(names)
		parts := make([]string, 0, len(names))
		for _, u := range names {
			parts = append(parts, fmt.Sprintf("%s in %s", u, strings.Join(units[u], "+")))
		}
		f.Detail = "sources disagree on unit: " + strings.Join(parts, "; ")
		return []Finding{f}
	}
	var unit string
	for u := range units {
		unit = u
	}
	base.Unit = unit

	var out []Finding

	if !hasL && (fieldQ > 0 || telQ > 0) {
		f := base
		f.Kind = Unbilled
		qty := telQ
		if qty == 0 {
			qty = fieldQ
		}
		f.Detail = "work recorded, no ledger line"
		if price, ok := latestPrice(assetLedger, k.date, unit); ok {
			c := cents(qty, price)
			f.AmountCents = &c
			f.Detail += fmt.Sprintf("; priced %s %s at last known %s/%s", qtyStr(qty), unit, priceStr(price), unit)
		} else {
			f.Detail += "; no earlier price for this asset in " + unit
		}
		out = append(out, f)
	}

	if hasL && !hasF && !hasT {
		f := base
		f.Kind = UnsupportedBilling
		var total int64
		ids := make([]string, 0, len(b.ledger))
		for _, r := range b.ledger {
			total += cents(r.Quantity, r.UnitPrice)
			ids = append(ids, r.InvoiceLineID)
		}
		total = -total
		f.AmountCents = &total
		f.Detail = "billed with no field record or telemetry: " + strings.Join(ids, ",")
		out = append(out, f)
	}

	if hasL && hasT && beyond(telQ-ledQ, telQ, opt.ToleranceMilliPct) {
		f := base
		f.Kind = QuantityMismatch
		price := b.ledger[len(b.ledger)-1].UnitPrice
		c := signedCents(telQ-ledQ, price)
		f.AmountCents = &c
		f.Detail = fmt.Sprintf("telemetry %s vs ledger %s %s at %s/%s", qtyStr(telQ), qtyStr(ledQ), unit, priceStr(price), unit)
		out = append(out, f)
	}

	if hasF && hasT && beyond(fieldQ-telQ, telQ, opt.ToleranceMilliPct) {
		f := base
		f.Kind = FieldTelemetryMismatch
		f.Detail = fmt.Sprintf("field %s vs telemetry %s %s", qtyStr(fieldQ), qtyStr(telQ), unit)
		out = append(out, f)
	}
	return out
}

// beyond reports whether |diff| exceeds tol (milli-percent) of base, in integers.
func beyond(diff, base, tolMilliPct int64) bool {
	if diff < 0 {
		diff = -diff
	}
	return diff*100_000 > tolMilliPct*base
}

// latestPrice returns the unit price of the most recent ledger line for the
// asset dated on or before date and billed in unit; the later line in file
// order wins a same-day tie.
func latestPrice(lines []ingest.LedgerRow, date, unit string) (int64, bool) {
	var best *ingest.LedgerRow
	for i := range lines {
		l := &lines[i]
		if l.Date > date || l.Unit != unit {
			continue
		}
		if best == nil || l.Date >= best.Date {
			best = l
		}
	}
	if best == nil {
		return 0, false
	}
	return best.UnitPrice, true
}

// cents prices a non-negative quantity (10^ingest.QtyScale) at a unit price
// (10^ingest.PriceScale), rounding half up to whole cents.
func cents(qty, price int64) int64 {
	const div = 100_000 // 10^(QtyScale+PriceScale) / 100 cents per dollar
	return (qty*price + div/2) / div
}

func signedCents(qty, price int64) int64 {
	if qty < 0 {
		return -cents(-qty, price)
	}
	return cents(qty, price)
}

func qtyStr(q int64) string   { return decimal.Format(q, ingest.QtyScale, 0) }
func priceStr(p int64) string { return "$" + decimal.Format(p, ingest.PriceScale, 2) }

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func amount(f Finding) int64 {
	if f.AmountCents == nil {
		return 0
	}
	return abs(*f.AmountCents)
}

func sortFindings(fs []Finding) {
	slices.SortFunc(fs, func(a, b Finding) int {
		if x, y := amount(a), amount(b); x != y {
			if x > y {
				return -1
			}
			return 1
		}
		if c := strings.Compare(a.Date, b.Date); c != 0 {
			return c
		}
		if c := strings.Compare(a.AssetID, b.AssetID); c != 0 {
			return c
		}
		return slices.Index(Kinds, a.Kind) - slices.Index(Kinds, b.Kind)
	})
}
