// Package report renders findings as a text table or JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/riftroot/tieout/internal/decimal"
	"github.com/riftroot/tieout/internal/ingest"
	"github.com/riftroot/tieout/internal/reconcile"
)

// Summary aggregates a scan. Exposure is the sum of absolute priced amounts.
type Summary struct {
	Counts        map[reconcile.Kind]int
	ExposureCents int64
	Unpriced      int
}

func Summarize(fs []reconcile.Finding) Summary {
	s := Summary{Counts: map[reconcile.Kind]int{}}
	for _, f := range fs {
		s.Counts[f.Kind]++
		if f.AmountCents == nil {
			s.Unpriced++
			continue
		}
		a := *f.AmountCents
		if a < 0 {
			a = -a
		}
		s.ExposureCents += a
	}
	return s
}

func usd(cents int64) string { return decimal.Format(cents, 2, 2) }

func Text(w io.Writer, fs []reconcile.Finding) error {
	s := Summarize(fs)
	if len(fs) == 0 {
		fmt.Fprintln(w, "No findings.")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "KIND\tASSET\tDATE\tAMOUNT_USD\tDETAIL")
		for _, f := range fs {
			amt := "unpriced"
			if f.AmountCents != nil {
				amt = usd(*f.AmountCents)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", f.Kind, f.AssetID, f.Date, amt, f.Detail)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Summary")
	for _, k := range reconcile.Kinds {
		fmt.Fprintf(w, "  %-25s %d\n", k, s.Counts[k])
	}
	fmt.Fprintf(w, "  %-25s %d\n", "total findings", len(fs))
	fmt.Fprintf(w, "  %-25s USD %s\n", "total priced exposure", usd(s.ExposureCents))
	_, err := fmt.Fprintf(w, "  %-25s %d\n", "unpriced findings", s.Unpriced)
	return err
}

type jsonFinding struct {
	Kind              reconcile.Kind `json:"kind"`
	AssetID           string         `json:"asset_id"`
	Date              string         `json:"date"`
	Unit              string         `json:"unit,omitempty"`
	FieldQuantity     *string        `json:"field_quantity"`
	TelemetryQuantity *string        `json:"telemetry_quantity"`
	LedgerQuantity    *string        `json:"ledger_quantity"`
	AmountCents       *int64         `json:"amount_cents"`
	AmountUSD         *string        `json:"amount_usd"`
	Detail            string         `json:"detail"`
}

type jsonSummary struct {
	Counts             map[reconcile.Kind]int `json:"counts"`
	Total              int                    `json:"total_findings"`
	TotalExposureCents int64                  `json:"total_priced_exposure_cents"`
	TotalExposureUSD   string                 `json:"total_priced_exposure_usd"`
	Unpriced           int                    `json:"unpriced"`
}

func str(q *int64) *string {
	if q == nil {
		return nil
	}
	s := decimal.Format(*q, ingest.QtyScale, 0)
	return &s
}

func JSON(w io.Writer, fs []reconcile.Finding) error {
	s := Summarize(fs)
	counts := map[reconcile.Kind]int{}
	for _, k := range reconcile.Kinds {
		counts[k] = s.Counts[k]
	}
	out := struct {
		Findings []jsonFinding `json:"findings"`
		Summary  jsonSummary   `json:"summary"`
	}{
		Findings: []jsonFinding{},
		Summary: jsonSummary{Counts: counts, Total: len(fs), TotalExposureCents: s.ExposureCents,
			TotalExposureUSD: usd(s.ExposureCents), Unpriced: s.Unpriced},
	}
	for _, f := range fs {
		jf := jsonFinding{Kind: f.Kind, AssetID: f.AssetID, Date: f.Date, Unit: f.Unit,
			FieldQuantity: str(f.Field), TelemetryQuantity: str(f.Telemetry), LedgerQuantity: str(f.Ledger),
			AmountCents: f.AmountCents, Detail: f.Detail}
		if f.AmountCents != nil {
			u := usd(*f.AmountCents)
			jf.AmountUSD = &u
		}
		out.Findings = append(out.Findings, jf)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
