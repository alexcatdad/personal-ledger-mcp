package ledger

import (
	"math"
	"testing"
)

func TestMoney(t *testing.T) {
	for _, v := range []int64{0, 1, -1, 12345, math.MaxInt64, math.MinInt64} {
		s := formatMoney(v)
		got, e := parseMoney(s)
		if e != nil || got != v {
			t.Fatalf("round trip %d: %s %d %v", v, s, got, e)
		}
	}
	for _, s := range []string{"1.001", "1e2", "NaN", " 1.00", "01.00", "92233720368547758.08", "-92233720368547758.09", ".01"} {
		if _, e := parseMoney(s); e == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
func TestSplitOverflow(t *testing.T) {
	in := CreateExpenseInput{Amount: "92233720368547758.07", OccurredDate: "2026-09-27", Allocations: []AllocationInput{{Amount: "92233720368547758.07", CategoryID: "a"}, {Amount: "0.01", CategoryID: "a"}}}
	if _, _, e := validateExpense(in); e == nil {
		t.Fatal("overflowing allocation sum accepted")
	}
}
func FuzzMoneyRoundTrip(f *testing.F) {
	f.Add(int64(43000))
	f.Add(int64(math.MinInt64))
	f.Fuzz(func(t *testing.T, v int64) {
		got, e := parseMoney(formatMoney(v))
		if e != nil || got != v {
			t.Fatalf("%d became %d %v", v, got, e)
		}
	})
}

func TestDateBounds(t *testing.T) {
	for _, v := range []string{"0000-01-01", "2026-02-29", "2026-9-01", "invalid"} {
		if date(v) == nil {
			t.Errorf("accepted invalid date %q", v)
		}
	}
	for _, v := range []string{"0001-01-01", "2024-02-29", "9999-12-31"} {
		if err := date(v); err != nil {
			t.Errorf("rejected valid date %q: %v", v, err)
		}
	}
}
