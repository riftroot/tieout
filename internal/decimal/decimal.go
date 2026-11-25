// Package decimal parses and formats fixed-point decimals held in int64, so
// quantities and prices never pass through a float.
package decimal

import (
	"fmt"
	"strings"
)

// Parse reads an unsigned decimal such as "12", "12.5" or "0.125" into an
// integer scaled by 10^scale. It rejects signs, exponents, empty parts, more
// than scale fractional digits, and integer parts longer than maxIntDigits.
func Parse(s string, scale, maxIntDigits int) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	intPart, fracPart, hasDot := strings.Cut(s, ".")
	if intPart == "" || (hasDot && fracPart == "") {
		return 0, fmt.Errorf("%q is not a decimal number", s)
	}
	if !allDigits(intPart) || !allDigits(fracPart) {
		return 0, fmt.Errorf("%q is not an unsigned decimal number", s)
	}
	if len(fracPart) > scale {
		return 0, fmt.Errorf("%q has more than %d decimal places", s, scale)
	}
	if len(strings.TrimLeft(intPart, "0")) > maxIntDigits {
		return 0, fmt.Errorf("%q is too large (more than %d integer digits)", s, maxIntDigits)
	}
	var v int64
	for _, c := range intPart + fracPart + strings.Repeat("0", scale-len(fracPart)) {
		v = v*10 + int64(c-'0')
	}
	return v, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Format renders v/10^scale, keeping at least minFrac fractional digits and
// dropping trailing zeros beyond that.
func Format(v int64, scale, minFrac int) string {
	neg := v < 0
	u := uint64(v)
	if neg {
		u = -u
	}
	digits := fmt.Sprintf("%0*d", scale+1, u)
	intPart, frac := digits[:len(digits)-scale], digits[len(digits)-scale:]
	for len(frac) > minFrac && frac[len(frac)-1] == '0' {
		frac = frac[:len(frac)-1]
	}
	out := intPart
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}
