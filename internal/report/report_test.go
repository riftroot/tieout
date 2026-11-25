package report

import (
	"errors"
	"io"
	"math"
	"testing"

	"github.com/riftroot/tieout/internal/reconcile"
)

func TestTotalOverflowFails(t *testing.T) {
	a, b := int64(math.MaxInt64), int64(1)
	fs := []reconcile.Finding{{Kind: reconcile.Unbilled, AmountCents: &a}, {Kind: reconcile.Unbilled, AmountCents: &b}}
	for name, render := range map[string]func(io.Writer, []reconcile.Finding) error{"text": Text, "json": JSON} {
		if err := render(io.Discard, fs); !errors.Is(err, reconcile.ErrOverflow) {
			t.Errorf("%s: want ErrOverflow, got %v", name, err)
		}
	}
}
