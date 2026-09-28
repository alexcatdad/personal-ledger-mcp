package ledger

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]{1,2})?$`)

func parseMoney(value string) (int64, error) {
	if !decimalPattern.MatchString(value) {
		return 0, fmt.Errorf("%w: money must be an exact decimal with at most two decimals", ErrValidation)
	}
	neg := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	p := strings.Split(value, ".")
	f := "00"
	if len(p) == 2 {
		f = p[1] + strings.Repeat("0", 2-len(p[1]))
	}
	n := new(big.Int)
	n.SetString(p[0]+f, 10)
	if neg {
		n.Neg(n)
	}
	if !n.IsInt64() {
		return 0, fmt.Errorf("%w: money out of range", ErrValidation)
	}
	return n.Int64(), nil
}
func formatMoney(v int64) string {
	n := big.NewInt(v)
	neg := n.Sign() < 0
	n.Abs(n)
	s := n.String()
	for len(s) < 3 {
		s = "0" + s
	}
	s = s[:len(s)-2] + "." + s[len(s)-2:]
	if neg {
		s = "-" + s
	}
	return s
}
