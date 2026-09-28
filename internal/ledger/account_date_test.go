package ledger

import (
	"errors"
	"testing"
	"time"
)

func TestAccountOptionalOpeningDate(t *testing.T) {
	s, ctx := testService(t)
	s.now = func() time.Time { return time.Date(2026, 9, 27, 22, 30, 0, 0, time.UTC) }
	in := CreateAccountInput{OperationKey: "optional-date", Name: "Bank", Currency: "RON", OpeningAmount: "100"}
	a, err := s.CreateAccount(ctx, in)
	if err != nil || a.OpeningDate != "2026-09-28" {
		t.Fatal(a, err)
	}
	s.now = func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }
	replay, err := s.CreateAccount(ctx, in)
	if err != nil || replay != a {
		t.Fatal(replay, err)
	}
	in.OpeningDate = "2026-09-28"
	if _, err = s.CreateAccount(ctx, in); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	in.OperationKey = "explicit-date"
	in.Name = "Historical"
	in.OpeningDate = "2026-01-01"
	a, err = s.CreateAccount(ctx, in)
	if err != nil || a.OpeningDate != in.OpeningDate {
		t.Fatal(a, err)
	}
	in.OperationKey = "invalid-date"
	in.OpeningDate = "2026-02-30"
	if _, err = s.CreateAccount(ctx, in); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}
