// Package decimal provides exact arithmetic for construction quantities and money.
package decimal

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Decimal stores an exact rational value. Its zero value is numeric zero.
// Division remains exact; callers must choose a rounding policy at a boundary.
// Decimal values are immutable: operations allocate independent results.
type Decimal struct {
	r *big.Rat
}

// Parse accepts a base-10 integer or decimal without exponent notation.
func Parse(value string) (Decimal, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return Decimal{}, errors.New("decimal must be a non-empty base-10 number")
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok || strings.ContainsAny(value, "/eE") {
		return Decimal{}, fmt.Errorf("invalid decimal %q", value)
	}
	return Decimal{r: r}, nil
}

// MustParse parses value and panics if it is not a valid decimal literal.
func MustParse(value string) Decimal {
	d, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return d
}

func (d Decimal) rat() *big.Rat {
	if d.r == nil {
		return new(big.Rat)
	}
	return new(big.Rat).Set(d.r)
}

// Add returns the exact sum of d and other.
func (d Decimal) Add(other Decimal) Decimal {
	return Decimal{r: new(big.Rat).Add(d.rat(), other.rat())}
}

// Sub returns the exact difference d - other.
func (d Decimal) Sub(other Decimal) Decimal {
	return Decimal{r: new(big.Rat).Sub(d.rat(), other.rat())}
}

// Mul returns the exact product of d and other.
func (d Decimal) Mul(other Decimal) Decimal {
	return Decimal{r: new(big.Rat).Mul(d.rat(), other.rat())}
}

// Quo returns the exact quotient of d and other. It returns an error for zero divisor.
func (d Decimal) Quo(other Decimal) (Decimal, error) {
	if other.Sign() == 0 {
		return Decimal{}, errors.New("division by zero")
	}
	return Decimal{r: new(big.Rat).Quo(d.rat(), other.rat())}, nil
}

// Cmp compares d and other, returning -1, 0, or 1.
func (d Decimal) Cmp(other Decimal) int {
	return d.rat().Cmp(other.rat())
}

// Sign returns -1, 0, or 1 according to the sign of d.
func (d Decimal) Sign() int {
	return d.rat().Sign()
}

// IsInteger reports whether d has no fractional part.
func (d Decimal) IsInteger() bool {
	return d.rat().IsInt()
}

// String returns a canonical exact representation. Non-terminating decimal
// rationals are represented as numerator/denominator and must be rounded
// explicitly before use in decimal-only external formats.
func (d Decimal) String() string {
	r := d.rat()
	if r.IsInt() {
		return r.Num().String()
	}
	den := new(big.Int).Set(r.Denom())
	for _, factor := range []int64{2, 5} {
		f := big.NewInt(factor)
		for {
			q, rem := new(big.Int), new(big.Int)
			q.QuoRem(den, f, rem)
			if rem.Sign() != 0 {
				break
			}
			den = q
		}
	}
	if den.Cmp(big.NewInt(1)) != 0 {
		return r.RatString()
	}
	return r.FloatString(decimalPlaces(r.Denom()))
}

func decimalPlaces(den *big.Int) int {
	two, five := 0, 0
	n := new(big.Int).Set(den)
	for n.Bit(0) == 0 {
		n.Rsh(n, 1)
		two++
	}
	f := big.NewInt(5)
	for {
		q, rem := new(big.Int), new(big.Int)
		q.QuoRem(n, f, rem)
		if rem.Sign() != 0 {
			break
		}
		n = q
		five++
	}
	if five > two {
		return five
	}
	return two
}

// StringFixed formats d to exactly places fractional digits using an explicit
// round-half-away-from-zero policy. Use only where that policy is approved.
func (d Decimal) StringFixed(places int) (string, error) {
	if places < 0 {
		return "", errors.New("decimal places cannot be negative")
	}
	return d.rat().FloatString(places), nil
}
