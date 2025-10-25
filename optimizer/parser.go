package optimizer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ParseSize parses size strings like "200k", "1.5M", "500kb"
func ParseSize(sizeStr string) (int64, error) {
	sizeStr = strings.ToLower(strings.TrimSpace(sizeStr))

	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([kmgt]?i?b?)$`)
	matches := re.FindStringSubmatch(sizeStr)

	if matches == nil {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value: %s", matches[1])
	}

	unit := matches[2]
	multiplier := int64(1)

	switch {
	case strings.HasPrefix(unit, "k"):
		multiplier = 1024
	case strings.HasPrefix(unit, "m"):
		multiplier = 1024 * 1024
	case strings.HasPrefix(unit, "g"):
		multiplier = 1024 * 1024 * 1024
	case strings.HasPrefix(unit, "t"):
		multiplier = 1024 * 1024 * 1024 * 1024
	}

	return int64(value * float64(multiplier)), nil
}
