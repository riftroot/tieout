# tieout

Rift Root's tie-out scanner. It reconciles three records that should agree and usually don't, and prices each gap in dollars:

- **field operations**: work orders, what crews did
- **telemetry**: meter, SCADA and GPS readings, what instruments measured
- **ledger**: billing and GL lines, what the books recognised

**Status: early development.** The input schemas and output format may change.

## Install and run

Go 1.26 or later, standard library only.

```
go build -o tieout ./cmd/tieout
tieout scan --field field.csv --telemetry telemetry.csv --ledger ledger.csv [--tolerance PCT] [--json]
```

| Flag | Meaning |
| --- | --- |
| `--field`, `--telemetry`, `--ledger` | the three input files (required) |
| `--tolerance` | allowed relative difference in percent, up to 3 decimals; default `2` |
| `--json` | machine-readable output instead of a text table |

Exit codes: `0` no findings, `1` findings exist, `2` input or usage error.

## Input schemas

All files are CSV with the header row shown. Dates are `YYYY-MM-DD`. Quantities are unsigned decimals with at most 3 decimal places and under 1,000,000; prices are unsigned USD with at most 4 decimal places and under 100,000. Every row is validated on load; the first bad row stops the run with `file:line: column: reason` and exit code 2. Ids (`work_order_id` in field, `invoice_line_id` in ledger) must be unique.

**field.csv**: crew-reported quantity delivered or serviced

```
work_order_id,asset_id,date,quantity,unit
```

**telemetry.csv**: measured quantity. Several readings for one asset-day are summed.

```
asset_id,date,quantity,unit
```

**ledger.csv**: billed lines. `work_order_id` may be empty.

```
invoice_line_id,work_order_id,asset_id,date,quantity,unit,unit_price_usd
```

## What it reports

Records are grouped per (`asset_id`, `date`). Findings, sorted by absolute dollar value (largest first):

| Kind | When | Priced as |
| --- | --- | --- |
| `unbilled` | field or telemetry shows work, ledger has no line | quantity (telemetry, else field) x the asset's most recent unit price on or before that date in the same unit; `unpriced` if there is none |
| `quantity_mismatch` | ledger and telemetry differ by more than the tolerance (relative to telemetry) | (telemetry - ledger) x ledger unit price |
| `unsupported_billing` | ledger line with no field record and no telemetry | the billed amount, negative |
| `field_telemetry_mismatch` | field and telemetry differ by more than the tolerance | unpriced, shown as evidence |
| `unit_mismatch` | sources use different units for the same asset-day | unpriced; units are never converted, and no other check runs for that asset-day |

Positive amounts are work not billed; negative amounts are billing without matching work. The summary's total priced exposure adds the absolute values. Money is held as integer cents (quantities and prices as scaled integers), never floats; amounts round half up to the cent. Ledger quantity is checked against telemetry only, so an asset-day with a ledger line and field record but no telemetry is not flagged.

## Example

`testdata/fleet/` holds a synthetic fleet: 10 assets over 30 days with a handful of seeded gaps.

```
$ tieout scan --field testdata/fleet/field.csv --telemetry testdata/fleet/telemetry.csv --ledger testdata/fleet/ledger.csv
KIND                      ASSET    DATE        AMOUNT_USD  DETAIL
unbilled                  GEN-201  2026-10-17  1027.80     work recorded, no ledger line; priced 5555.678 kWh at last known $0.185/kWh
unbilled                  WTR-105  2026-10-09  395.66      work recorded, no ledger line; priced 335.309 m3 at last known $1.18/m3
unbilled                  WTR-101  2026-10-05  186.70      work recorded, no ledger line; priced 149.357 m3 at last known $1.25/m3
unbilled                  WTR-102  2026-10-12  101.07      work recorded, no ledger line; priced 80.854 m3 at last known $1.25/m3
quantity_mismatch         WTR-103  2026-10-19  50.55       telemetry 208.867 vs ledger 167.093 m3 at $1.21/m3
unsupported_billing       WTR-108  2026-10-11  -50.00      billed with no field record or telemetry: INV-5076
quantity_mismatch         GEN-201  2026-10-22  -45.84      telemetry 2752.975 vs ledger 3000.742 kWh at $0.185/kWh
quantity_mismatch         WTR-104  2026-10-20  6.08        telemetry 38.357 vs ledger 33.754 m3 at $1.32/m3
unbilled                  GEN-202  2026-10-01  unpriced    work recorded, no ledger line; no earlier price for this asset in kWh
unbilled                  GEN-202  2026-10-02  unpriced    work recorded, no ledger line; no earlier price for this asset in kWh
unbilled                  GEN-202  2026-10-03  unpriced    work recorded, no ledger line; no earlier price for this asset in kWh
field_telemetry_mismatch  WTR-107  2026-10-08  unpriced    field 117.997 vs telemetry 98.331 m3
unit_mismatch             WTR-108  2026-10-25  unpriced    sources disagree on unit: gal in ledger; m3 in field+telemetry

Summary
  unbilled                  7
  quantity_mismatch         3
  unsupported_billing       1
  field_telemetry_mismatch  1
  unit_mismatch             1
  total findings            13
  total priced exposure     USD 1863.70
  unpriced findings         5
$ echo $?
1
```

`--json` emits the same findings and summary as one JSON document; see `testdata/golden/scan.json`.

A malformed row stops the run:

```
$ tieout scan --field testdata/malformed/field.csv --telemetry testdata/fleet/telemetry.csv --ledger testdata/fleet/ledger.csv
tieout: testdata/malformed/field.csv:3: date: "2026-13-45" is not a YYYY-MM-DD date
$ echo $?
2
```

## Tests

```
go vet ./...
go test ./...
go test ./cmd/tieout -update   # rewrite golden files after an intended output change
```

All sample data is synthetic.

## Licence

Apache License 2.0; see [LICENSE](LICENSE). Copyright 2026 Rift Root LLC.
