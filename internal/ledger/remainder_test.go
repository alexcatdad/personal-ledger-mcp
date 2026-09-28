package ledger

import (
	"errors"
	"testing"
)

func TestRemainderValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		total string
		lines []AllocationInput
		want  int64
		valid bool
	}{
		{"only", "430", []AllocationInput{{Remainder: true, CategoryID: "c"}}, 43000, true},
		{"last", "430", []AllocationInput{{Amount: "250", CategoryID: "c"}, {Remainder: true, CategoryID: "c"}}, 18000, true},
		{"precision", "0.03", []AllocationInput{{Amount: "0.01", CategoryID: "c"}, {Remainder: true, CategoryID: "c"}}, 2, true},
		{"max", "92233720368547758.07", []AllocationInput{{Amount: "0.01", CategoryID: "c"}, {Remainder: true, CategoryID: "c"}}, 9223372036854775806, true},
		{"two", "430", []AllocationInput{{Remainder: true, CategoryID: "c"}, {Remainder: true, CategoryID: "c"}}, 0, false},
		{"both", "430", []AllocationInput{{Remainder: true, Amount: "430", CategoryID: "c"}}, 0, false},
		{"implicit", "430", []AllocationInput{{CategoryID: "c"}}, 0, false},
		{"missing category", "430", []AllocationInput{{Remainder: true}}, 0, false},
		{"zero", "250", []AllocationInput{{Amount: "250", CategoryID: "c"}, {Remainder: true, CategoryID: "c"}}, 0, false},
		{"negative", "249", []AllocationInput{{Amount: "250", CategoryID: "c"}, {Remainder: true, CategoryID: "c"}}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, parts, err := validateExpense(CreateExpenseInput{Amount: tc.total, OccurredDate: "2026-09-27", Allocations: tc.lines})
			if !tc.valid {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("expected validation: %v", err)
				}
				return
			}
			if err != nil || parts[len(parts)-1] != tc.want {
				t.Fatalf("%v %v", parts, err)
			}
		})
	}
}
