package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

var fleet = []string{
	"--field", "testdata/fleet/field.csv",
	"--telemetry", "testdata/fleet/telemetry.csv",
	"--ledger", "testdata/fleet/ledger.csv",
}

func exec(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Chdir("../..")
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from golden file (run go test ./cmd/tieout -update to refresh)\n--- got ---\n%s", name, got)
	}
}

func TestGoldenText(t *testing.T) {
	code, out, errs := exec(t, append([]string{"scan"}, fleet...)...)
	if code != 1 || errs != "" {
		t.Fatalf("exit = %d, stderr = %q; want 1 and empty", code, errs)
	}
	golden(t, "scan.txt", out)
}

func TestGoldenJSON(t *testing.T) {
	code, out, errs := exec(t, append([]string{"scan", "--json"}, fleet...)...)
	if code != 1 || errs != "" {
		t.Fatalf("exit = %d, stderr = %q; want 1 and empty", code, errs)
	}
	golden(t, "scan.json", out)
}

func TestCleanExitsZero(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	f := write("f.csv", "work_order_id,asset_id,date,quantity,unit\nW1,A,2026-10-01,10,m3\n")
	tel := write("t.csv", "asset_id,date,quantity,unit\nA,2026-10-01,4,m3\nA,2026-10-01,6,m3\n")
	l := write("l.csv", "invoice_line_id,work_order_id,asset_id,date,quantity,unit,unit_price_usd\nI1,W1,A,2026-10-01,10,m3,1.25\n")
	code, out, errs := exec(t, "scan", "--field", f, "--telemetry", tel, "--ledger", l)
	if code != 0 || errs != "" || !strings.HasPrefix(out, "No findings.") {
		t.Fatalf("exit = %d, stderr = %q, stdout = %q; want 0, empty, No findings.", code, errs, out)
	}
}

func TestMalformedRowExitsTwoWithFileAndLine(t *testing.T) {
	code, out, errs := exec(t, "scan",
		"--field", "testdata/malformed/field.csv",
		"--telemetry", "testdata/fleet/telemetry.csv",
		"--ledger", "testdata/fleet/ledger.csv")
	const want = `tieout: testdata/malformed/field.csv:3: date: "2026-13-45" is not a YYYY-MM-DD date`
	if code != 2 || out != "" || strings.TrimSpace(errs) != want {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q; want 2, empty, %q", code, out, errs, want)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"scan"},
		{"scan", "--tolerance", "abc"},
		{"scan", "--tolerance", "101", "--field", "a", "--telemetry", "b", "--ledger", "c"},
		{"scan", "--field", "nope.csv", "--telemetry", "testdata/fleet/telemetry.csv", "--ledger", "testdata/fleet/ledger.csv"},
	} {
		if code, _, _ := exec(t, args...); code != 2 {
			t.Errorf("args %v: exit = %d, want 2", args, code)
		}
	}
}

func TestToleranceFlagChangesFindings(t *testing.T) {
	_, strict, _ := exec(t, append([]string{"scan", "--tolerance", "0"}, fleet...)...)
	_, loose, _ := exec(t, append([]string{"scan", "--tolerance", "50"}, fleet...)...)
	if strict == loose {
		t.Error("tolerance 0 and 50 produced identical output")
	}
}
