package decimal

import "testing"

func TestParse(t *testing.T) {
	good := []struct {
		in   string
		want int64
	}{
		{"0", 0}, {"12", 12000}, {"12.5", 12500}, {"0.125", 125}, {"007.010", 7010}, {"999999.999", 999999999},
	}
	for _, c := range good {
		got, err := Parse(c.in, 3, 6)
		if err != nil || got != c.want {
			t.Errorf("Parse(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
	bad := []string{"", ".5", "5.", "-1", "+1", "1e3", "1,5", "1.2345", "1000000", "abc", "1.2.3", " 1", "0x10"}
	for _, in := range bad {
		if got, err := Parse(in, 3, 6); err == nil {
			t.Errorf("Parse(%q) = %d, nil; want error", in, got)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		v              int64
		scale, minFrac int
		want           string
	}{
		{12000, 3, 0, "12"}, {12500, 3, 0, "12.5"}, {125, 3, 0, "0.125"}, {5, 2, 2, "0.05"},
		{-1205, 2, 2, "-12.05"}, {0, 2, 2, "0.00"}, {100, 2, 2, "1.00"},
	}
	for _, c := range cases {
		if got := Format(c.v, c.scale, c.minFrac); got != c.want {
			t.Errorf("Format(%d,%d,%d) = %q; want %q", c.v, c.scale, c.minFrac, got, c.want)
		}
	}
}
