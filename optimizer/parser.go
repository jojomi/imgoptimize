package optimizer

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// sizePattern splits a size into number and unit, e.g. "1.5 MiB" into "1.5"
// and "mib". Only a dot is accepted as decimal separator.
var sizePattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([a-z]*)$`)

// unitMultipliers maps a lowercased unit to its factor. SI units count in
// powers of 1000, IEC units (with "i") in powers of 1024.
var unitMultipliers = map[string]float64{
	"": 1, "b": 1,
	"k": 1e3, "kb": 1e3, "ki": 1 << 10, "kib": 1 << 10,
	"m": 1e6, "mb": 1e6, "mi": 1 << 20, "mib": 1 << 20,
	"g": 1e9, "gb": 1e9, "gi": 1 << 30, "gib": 1 << 30,
	"t": 1e12, "tb": 1e12, "ti": 1 << 40, "tib": 1 << 40,
}

// ParseSize parses a target file size like "200k", "1.5MB" or "500KiB" into
// bytes, rounded to whole bytes. Units are case-insensitive.
func ParseSize(sizeStr string) (int64, error) {
	normalized := strings.ToLower(strings.TrimSpace(sizeStr))

	if strings.Contains(normalized, ",") {
		return 0, fmt.Errorf("invalid size %q: use a dot as decimal separator, e.g. 1.5M", sizeStr)
	}

	matches := sizePattern.FindStringSubmatch(normalized)
	if matches == nil {
		return 0, fmt.Errorf("invalid size %q: expected a number with an optional unit, e.g. 200k or 1.5MiB", sizeStr)
	}

	multiplier, known := unitMultipliers[matches[2]]
	if !known {
		return 0, fmt.Errorf("invalid size %q: unknown unit %q, use b, k, kB, Ki or KiB (likewise M, G, T)", sizeStr, matches[2])
	}

	// The pattern only lets valid numbers through, so parsing cannot fail.
	value, _ := strconv.ParseFloat(matches[1], 64)
	size := math.Round(value * multiplier)

	if size < 1 {
		return 0, fmt.Errorf("invalid size %q: must be at least 1 byte", sizeStr)
	}
	if size >= math.MaxInt64 {
		return 0, fmt.Errorf("invalid size %q: too large", sizeStr)
	}

	return int64(size), nil
}
