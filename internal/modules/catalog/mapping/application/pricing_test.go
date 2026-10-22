package application

import (
	"testing"

	"github.com/dujiao-next/internal/constants"

	"github.com/shopspring/decimal"
)

func TestCalculateMarkedUpPrice(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
		markup   string
		rounding string
		expected string
	}{
		{"zero markup returns original", "10.00", "0", "none", "10.00"},
		{"50% markup", "10.00", "50", "none", "15.00"},
		{"100% markup doubles price", "10.00", "100", "none", "20.00"},
		{"200% markup triples price", "10.00", "200", "none", "30.00"},
		{"small price with markup", "0.01", "100", "none", "0.02"},
		{"fractional result rounds to 2 decimals", "10.00", "33", "none", "13.30"},
		{"negative markup floors at zero", "5.00", "-200", "none", "0"},
		{"ceil_int rounds up", "10.00", "23.4", "ceil_int", "13.00"},
		{"ceil_int exact integer stays", "10.00", "100", "ceil_int", "20"},
		{"ceil_tenth rounds up to 0.1", "10.00", "23.4", "ceil_tenth", "12.40"},
		{"ceil_tenth exact tenth stays", "10.00", "50", "ceil_tenth", "15.00"},
		{"zero price stays zero", "0.00", "100", "none", "0.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := decimal.RequireFromString(tt.upstream)
			markup := decimal.RequireFromString(tt.markup)
			expected := decimal.RequireFromString(tt.expected)

			result := CalculateMarkedUpPrice(upstream, markup, tt.rounding)
			if !result.Equal(expected) {
				t.Errorf("CalculateMarkedUpPrice(%s, %s%%, %s) = %s, want %s",
					tt.upstream, tt.markup, tt.rounding, result.String(), tt.expected)
			}
		})
	}
}

func TestCalculateLocalPrice(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
		rate     string
		markup   string
		rounding string
		expected string
	}{
		{"USD to CNY with markup", "1.00", "7.2", "50", "none", "10.80"},
		{"rate 1 same as no conversion", "10.00", "1", "50", "none", "15.00"},
		{"rate 0 treated as 1", "10.00", "0", "50", "none", "15.00"},
		{"negative rate treated as 1", "10.00", "-1", "50", "none", "15.00"},
		{"rate with no markup", "1.00", "7.2", "0", "none", "7.20"},
		{"rate with ceil_int", "1.00", "7.2", "50", "ceil_int", "11.00"},
		{"rate with ceil_tenth", "1.00", "7.35", "0", "ceil_tenth", "7.35"},
		{"rate with ceil_tenth fractional", "1.00", "7.34", "1", "ceil_tenth", "7.50"},
		{"high precision rate", "100.00", "0.138462", "0", "none", "13.85"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := decimal.RequireFromString(tt.upstream)
			rate := decimal.RequireFromString(tt.rate)
			markup := decimal.RequireFromString(tt.markup)
			expected := decimal.RequireFromString(tt.expected)

			result := CalculateLocalPrice(upstream, rate, markup, decimal.Zero, "", tt.rounding)
			if !result.Equal(expected) {
				t.Errorf("CalculateLocalPrice(%s, rate=%s, %s%%, %s) = %s, want %s",
					tt.upstream, tt.rate, tt.markup, tt.rounding, result.String(), tt.expected)
			}
		})
	}
}

func TestCalculateFixedAmountPrice(t *testing.T) {
	tests := []struct{ name, upstream, rate, amount, rounding, want string }{
		{"per unit", "5", "1", "3", "none", "8"},
		{"convert before addition", "5", "7.2", "3", "none", "39"},
		{"decimal addition", "0.10", "1", "0.20", "none", "0.30"},
		{"round after addition", "1", "7.345", "0.01", "none", "7.36"},
		{"ceil integer", "5.01", "1", "3.20", "ceil_int", "9"},
		{"ceil tenth", "5.01", "1", "3.20", "ceil_tenth", "8.30"},
		{"exact integer", "5", "1", "3", "ceil_int", "8"},
		{"zero amount skips ceiling", "5.01", "1", "0", "ceil_int", "5.01"},
		{"zero amount still converts", "1", "7.345", "0", "ceil_tenth", "7.35"},
		{"zero upstream is valid", "0", "7.2", "3", "none", "3"},
		{"invalid rate retains fallback", "5", "0", "3", "none", "8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateLocalPrice(decimal.RequireFromString(tt.upstream), decimal.RequireFromString(tt.rate), decimal.NewFromInt(40), decimal.RequireFromString(tt.amount), constants.ConnectionPriceMarkupFixedAmount, tt.rounding)
			if !got.Equal(decimal.RequireFromString(tt.want)) {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
	// The saved value of the inactive mode must never affect the selected rule.
	for _, mode := range []string{"", constants.ConnectionPriceMarkupPercent} {
		got := CalculateLocalPrice(decimal.NewFromInt(5), decimal.NewFromInt(2), decimal.NewFromInt(40), decimal.NewFromInt(100), mode, "none")
		if !got.Equal(decimal.NewFromInt(14)) {
			t.Fatalf("mode %q: got %s, want 14", mode, got)
		}
	}
}
