package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronExpr represents a parsed 5-field cron expression.
type CronExpr struct {
	minute []int // 0-59
	hour   []int // 0-23
	dom    []int // 1-31
	month  []int // 1-12
	dow    []int // 0-6 (0=Sunday)
}

// ParseCron parses a 5-field cron expression: minute hour day-of-month month day-of-week.
// Also supports: @hourly, @daily, @weekly, @monthly.
func ParseCron(expr string) (*CronExpr, error) {
	expr = strings.TrimSpace(expr)

	// Handle special strings
	switch expr {
	case "@hourly":
		expr = "0 * * * *"
	case "@daily":
		expr = "0 0 * * *"
	case "@weekly":
		expr = "0 0 * * 0"
	case "@monthly":
		expr = "0 0 1 * *"
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: expected 5 fields, got %d", len(fields))
	}

	c := &CronExpr{}
	var err error

	c.minute, err = parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("cron minute: %w", err)
	}
	c.hour, err = parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("cron hour: %w", err)
	}
	c.dom, err = parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-month: %w", err)
	}
	c.month, err = parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("cron month: %w", err)
	}
	c.dow, err = parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-week: %w", err)
	}

	// Enforce minimum interval of 5 minutes to prevent cost amplification.
	// A schedule that fires every minute would trigger expensive LLM agent calls
	// at a rate that could cause significant cost and resource exhaustion.
	if err := c.enforceMinInterval(); err != nil {
		return nil, err
	}

	return c, nil
}

// MinCronIntervalMinutes is the minimum allowed interval between cron fires.
// Schedules more frequent than this are rejected to prevent cost amplification.
var MinCronIntervalMinutes = 5

// enforceMinInterval checks that the cron expression does not fire more
// frequently than MinCronIntervalMinutes. This prevents denial-of-service
// via high-frequency schedules that each spawn an expensive agent run.
func (c *CronExpr) enforceMinInterval() error {
	if MinCronIntervalMinutes <= 0 {
		return nil // Enforcement disabled.
	}
	// Use a reference time to check the gap between consecutive fires.
	ref := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	first := c.Next(ref)
	if first.IsZero() {
		return nil // No next fire; nothing to enforce.
	}
	second := c.Next(first)
	if second.IsZero() {
		return nil // Single fire; fine.
	}
	gap := second.Sub(first)
	if gap < time.Duration(MinCronIntervalMinutes)*time.Minute {
		return fmt.Errorf("cron: interval %v is below minimum %d minutes", gap, MinCronIntervalMinutes)
	}
	return nil
}

// String returns the cron expression as a string.
func (c *CronExpr) String() string {
	return fmt.Sprintf("%s %s %s %s %s",
		fieldString(c.minute), fieldString(c.hour),
		fieldString(c.dom), fieldString(c.month), fieldString(c.dow))
}

// Next returns the next time after `from` that matches the cron expression.
func (c *CronExpr) Next(from time.Time) time.Time {
	// Start from the next minute
	t := from.Truncate(time.Minute).Add(time.Minute)

	// Brute-force search up to 4 years (covers all month/dow combos)
	limit := t.Add(4 * 365 * 24 * time.Hour)
	for t.Before(limit) {
		if !contains(c.month, int(t.Month())) {
			// Jump to first day of next month
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(c.dom, t.Day()) || !contains(c.dow, int(t.Weekday())) {
			t = t.AddDate(0, 0, 1)
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(c.hour, t.Hour()) {
			t = t.Add(time.Hour)
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
			continue
		}
		if !contains(c.minute, t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{} // Should not happen for valid cron expressions
}

// parseField parses a single cron field (supports *, ranges, lists, steps).
func parseField(field string, min, max int) ([]int, error) {
	if field == "*" {
		return makeRange(min, max), nil
	}

	var result []int
	for _, part := range strings.Split(field, ",") {
		// Check for step: */N or M-N/S
		if idx := strings.Index(part, "/"); idx >= 0 {
			base := part[:idx]
			stepStr := part[idx+1:]
			step, err := strconv.Atoi(stepStr)
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("invalid step %q", stepStr)
			}
			var rangeVals []int
			if base == "*" {
				rangeVals = makeRange(min, max)
			} else {
				rangeVals, err = parseRange(base, min, max)
				if err != nil {
					return nil, err
				}
			}
			for i := 0; i < len(rangeVals); i += step {
				result = append(result, rangeVals[i])
			}
			continue
		}

		// Check for range: M-N
		if strings.Contains(part, "-") {
			vals, err := parseRange(part, min, max)
			if err != nil {
				return nil, err
			}
			result = append(result, vals...)
			continue
		}

		// Single value
		v, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid value %q", part)
		}
		if v < min || v > max {
			return nil, fmt.Errorf("value %d out of range [%d, %d]", v, min, max)
		}
		result = append(result, v)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("empty field")
	}
	return result, nil
}

func parseRange(s string, min, max int) ([]int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range %q", s)
	}
	lo, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid range start %q", parts[0])
	}
	hi, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid range end %q", parts[1])
	}
	if lo < min || hi > max || lo > hi {
		return nil, fmt.Errorf("range %d-%d out of bounds [%d, %d]", lo, hi, min, max)
	}
	return makeRange(lo, hi), nil
}

func makeRange(min, max int) []int {
	result := make([]int, 0, max-min+1)
	for i := min; i <= max; i++ {
		result = append(result, i)
	}
	return result
}

func contains(vals []int, v int) bool {
	for _, x := range vals {
		if x == v {
			return true
		}
	}
	return false
}

func fieldString(vals []int) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}
