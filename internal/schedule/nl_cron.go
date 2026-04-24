package schedule

import (
	"context"
	"fmt"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// CronConverter is an optional model-backed converter for NL→cron fallback.
type CronConverter interface {
	Complete(ctx context.Context, messages []agent.Message) (agent.Response, error)
}

// NLToCronWithModel tries rule-based conversion first, then falls back to the LLM.
// If model is nil, behaves identically to NLToCron.
func NLToCronWithModel(ctx context.Context, natural string, model CronConverter) (string, error) {
	// Try rules first (free, instant).
	cron, err := NLToCron(natural)
	if err == nil {
		return cron, nil
	}

	// No model → return the rule-based error.
	if model == nil {
		return "", err
	}

	// LLM fallback with structured prompt.
	prompt := fmt.Sprintf(
		"Convert this natural language schedule to a 5-field cron expression (min hour dom mon dow).\n"+
			"Reply with ONLY the cron expression, nothing else.\n\n"+
			"Input: %q\n\n"+
			"Rules:\n"+
			"- Standard 5-field cron: minute hour day-of-month month day-of-week\n"+
			"- Day of week: 0=Sunday, 1=Monday, ..., 6=Saturday\n"+
			"- Use * for every, ranges like 1-5, lists like 0,6\n"+
			"- Default time if not specified: 09:00", natural)

	resp, llmErr := model.Complete(ctx, []agent.Message{
		{Role: "user", Content: prompt},
	})
	if llmErr != nil {
		return "", fmt.Errorf("schedule: LLM conversion failed: %w", llmErr)
	}

	// Validate the LLM's output.
	cronExpr := strings.TrimSpace(resp.Content)
	if _, parseErr := ParseCron(cronExpr); parseErr != nil {
		return "", fmt.Errorf("schedule: LLM returned invalid cron %q: %w", cronExpr, parseErr)
	}

	return cronExpr, nil
}

// NLToCron converts a natural language schedule description to a cron expression.
// This is a rule-based converter for common patterns. Complex patterns should
// use the LLM-backed conversion via the model package.
func NLToCron(natural string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(natural))

	switch {
	case s == "every minute":
		return "*/5 * * * *", nil // Every 5 minutes (minimum enforced interval)
	case s == "every hour" || s == "hourly":
		return "0 * * * *", nil
	case s == "every day" || s == "daily":
		return "0 9 * * *", nil
	case s == "every morning":
		return "0 7 * * *", nil
	case s == "every evening":
		return "0 18 * * *", nil
	case s == "every night":
		return "0 21 * * *", nil
	case s == "every week" || s == "weekly":
		return "0 9 * * 1", nil // Monday 9am
	case s == "every month" || s == "monthly":
		return "0 9 1 * *", nil // 1st of month 9am
	case s == "every weekday":
		return "0 9 * * 1-5", nil
	case s == "every weekend":
		return "0 10 * * 0,6", nil
	}

	// Pattern: "every day at HH:MM"
	if strings.HasPrefix(s, "every day at ") {
		timeStr := strings.TrimPrefix(s, "every day at ")
		h, m, err := parseTime(timeStr)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * *", m, h), nil
	}

	// Pattern: "every <weekday> at HH:MM"
	days := map[string]int{
		"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3,
		"thursday": 4, "friday": 5, "saturday": 6,
	}
	for day, num := range days {
		prefix := "every " + day
		if strings.HasPrefix(s, prefix) {
			timeStr := strings.TrimPrefix(s, prefix)
			timeStr = strings.TrimPrefix(timeStr, " at ")
			if timeStr == "" || timeStr == s {
				return fmt.Sprintf("0 9 * * %d", num), nil
			}
			h, m, err := parseTime(timeStr)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d %d * * %d", m, h, num), nil
		}
	}

	return "", fmt.Errorf("schedule: cannot parse %q — use a cron expression directly", natural)
}

func parseTime(s string) (hour, min int, err error) {
	s = strings.TrimSpace(s)
	n, e := fmt.Sscanf(s, "%d:%d", &hour, &min)
	if e != nil || n != 2 {
		return 0, 0, fmt.Errorf("schedule: invalid time %q (use HH:MM)", s)
	}
	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("schedule: time out of range %q", s)
	}
	return hour, min, nil
}
