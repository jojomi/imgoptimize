package optimizer

import (
	"strings"
	"testing"
)

func TestParseSizeAcceptsSIAndIECUnits(t *testing.T) {
	cases := map[string]int64{
		"500":     500,
		"500b":    500,
		"500B":    500,
		"200k":    200_000,
		"200kb":   200_000,
		"200 kB":  200_000,
		"200ki":   204_800,
		"200KiB":  204_800,
		"1.5M":    1_500_000,
		"1.5MB":   1_500_000,
		"1.5Mi":   1_572_864,
		"1.5MiB":  1_572_864,
		"2g":      2_000_000_000,
		"2GiB":    2_147_483_648,
		"1t":      1_000_000_000_000,
		"1TiB":    1_099_511_627_776,
		" 300k ":  300_000,
		"0.5k":    500,
		"1.25MiB": 1_310_720,
		"1.0005k": 1_001, // auf ganze Bytes gerundet
	}
	for input, want := range cases {
		got, err := ParseSize(input)
		if err != nil {
			t.Errorf("ParseSize(%q): unexpected error: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseSize(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestParseSizeRejectsInvalidInput(t *testing.T) {
	cases := map[string]string{
		"":          "invalid size",
		"abc":       "invalid size",
		"k":         "invalid size",
		"-5k":       "invalid size",
		"1.5.0M":    "invalid size",
		"200x":      "unknown unit",
		"200kbit":   "unknown unit",
		"0":         "at least 1 byte",
		"0k":        "at least 1 byte",
		"0.4":       "at least 1 byte",
		"10000000t": "too large",
		// Ein deutsches Dezimalkomma darf nicht still falsch gelesen werden.
		"1,5M": "dot as decimal separator",
	}
	for input, wantErr := range cases {
		got, err := ParseSize(input)
		if err == nil {
			t.Errorf("ParseSize(%q) = %d, want error containing %q", input, got, wantErr)
			continue
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Errorf("ParseSize(%q) error = %q, want it to contain %q", input, err, wantErr)
		}
	}
}
