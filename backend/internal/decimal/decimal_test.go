package decimal

import "testing"

func TestParseAndExactArithmetic(t *testing.T) {
	cumulative := MustParse("63.00").Add(MustParse("19.04"))
	if want := "82.04"; cumulative.String() != want {
		t.Fatalf("cumulative quantity = %q, want %q", cumulative.String(), want)
	}

	got := cumulative.Sub(MustParse("19.04"))
	if want := "63"; got.String() != want {
		t.Fatalf("previous quantity = %q, want %q", got.String(), want)
	}

	amount := MustParse("82.04").Mul(MustParse("1200.50"))
	if want := "98489.02"; amount.String() != want {
		t.Fatalf("amount = %q, want %q", amount.String(), want)
	}
}

func TestZeroValueAndComparison(t *testing.T) {
	var zero Decimal
	if got := zero.String(); got != "0" {
		t.Fatalf("zero String() = %q, want 0", got)
	}
	if got := zero.Add(MustParse("0.01")).Cmp(MustParse("0.01")); got != 0 {
		t.Fatalf("comparison = %d, want 0", got)
	}
}

func TestQuoPreservesExactRational(t *testing.T) {
	got, err := MustParse("1").Quo(MustParse("3"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "1/3"; got.String() != want {
		t.Fatalf("quotient = %q, want %q", got.String(), want)
	}
}

func TestQuoRejectsZero(t *testing.T) {
	if _, err := MustParse("1").Quo(Decimal{}); err == nil {
		t.Fatal("division by zero succeeded")
	}
}

func TestStringFixedRounding(t *testing.T) {
	for _, tc := range []struct {
		value  string
		places int
		want   string
	}{
		{value: "1.005", places: 2, want: "1.01"},
		{value: "-1.005", places: 2, want: "-1.01"},
		{value: "1.004", places: 2, want: "1.00"},
		{value: "1.5", places: 0, want: "2"},
	} {
		got, err := MustParse(tc.value).StringFixed(tc.places)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("StringFixed(%q, %d) = %q, want %q", tc.value, tc.places, got, tc.want)
		}
	}
}

func TestParseRejectsInvalidInputs(t *testing.T) {
	for _, value := range []string{"", " 1", "1 ", "1/2", "1e3", "NaN", "1.2.3"} {
		if _, err := Parse(value); err == nil {
			t.Errorf("Parse(%q) succeeded", value)
		}
	}
}
