// Command tieout reconciles field, telemetry and ledger records.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/riftroot/tieout/internal/decimal"
	"github.com/riftroot/tieout/internal/ingest"
	"github.com/riftroot/tieout/internal/reconcile"
	"github.com/riftroot/tieout/internal/report"
)

const usage = "usage: tieout scan --field f.csv --telemetry t.csv --ledger l.csv [--tolerance PCT] [--json]"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run returns the exit code: 0 clean, 1 findings, 2 usage or input error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "scan" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fieldPath := fs.String("field", "", "field operations CSV")
	telPath := fs.String("telemetry", "", "telemetry CSV")
	ledgerPath := fs.String("ledger", "", "ledger CSV")
	asJSON := fs.Bool("json", false, "emit JSON instead of a text table")
	tol := int64(2000)
	fs.Func("tolerance", "allowed difference in percent, up to 3 decimals (default 2)", func(s string) error {
		v, err := decimal.Parse(s, 3, 3)
		if err != nil {
			return err
		}
		if v > 100_000 {
			return fmt.Errorf("%q is above 100 percent", s)
		}
		tol = v
		return nil
	})
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *fieldPath == "" || *telPath == "" || *ledgerPath == "" {
		fmt.Fprintln(stderr, "tieout: --field, --telemetry and --ledger are all required")
		fmt.Fprintln(stderr, usage)
		return 2
	}

	data, err := ingest.Load(*fieldPath, *telPath, *ledgerPath)
	if err != nil {
		fmt.Fprintln(stderr, "tieout:", err)
		return 2
	}
	findings, err := reconcile.Scan(data, reconcile.Options{ToleranceMilliPct: tol})
	if err != nil {
		fmt.Fprintln(stderr, "tieout:", err)
		return 2
	}

	render := report.Text
	if *asJSON {
		render = report.JSON
	}
	if err := render(stdout, findings); err != nil {
		fmt.Fprintln(stderr, "tieout: writing output:", err)
		return 2
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}
