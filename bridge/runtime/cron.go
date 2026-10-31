package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var taskCronParser = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func parseCron(expr string) (cron.Schedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 && len(parts) != 6 {
		return nil, fmt.Errorf("cron must have 5 or 6 fields (seconds are optional)")
	}
	weekday, err := normalizeCronWeekday(parts[len(parts)-1])
	if err != nil {
		return nil, err
	}
	parts[len(parts)-1] = weekday
	return taskCronParser.Parse(strings.Join(parts, " "))
}

// Preserve Sunday=7 for existing tasks and the frontend Croner parser.
func normalizeCronWeekday(field string) (string, error) {
	field = strings.ToUpper(field)
	for index, name := range []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"} {
		field = strings.ReplaceAll(field, name, strconv.Itoa(index))
	}
	var normalized []string
	for _, part := range strings.Split(field, ",") {
		if !strings.Contains(part, "7") {
			normalized = append(normalized, part)
			continue
		}
		stepParts := strings.Split(part, "/")
		if len(stepParts) > 2 {
			return "", fmt.Errorf("invalid cron weekday %q", part)
		}
		// Leave wildcard steps intact to retain day-of-month/day-of-week semantics.
		if stepParts[0] == "*" || stepParts[0] == "?" {
			normalized = append(normalized, part)
			continue
		}
		step := 1
		if len(stepParts) == 2 {
			var err error
			step, err = strconv.Atoi(stepParts[1])
			if err != nil || step < 1 {
				return "", fmt.Errorf("invalid cron weekday step %q", part)
			}
		}
		bounds := strings.Split(stepParts[0], "-")
		if len(bounds) > 2 {
			return "", fmt.Errorf("invalid cron weekday %q", part)
		}
		start, err := strconv.Atoi(bounds[0])
		if err != nil || start < 0 || start > 7 {
			return "", fmt.Errorf("invalid cron weekday %q", part)
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(bounds[1])
		} else if len(stepParts) == 2 {
			end = 7
		}
		if err != nil || end < start || end > 7 {
			return "", fmt.Errorf("invalid cron weekday %q", part)
		}
		for value := start; value <= end; {
			normalized = append(normalized, strconv.Itoa(value%7))
			if step > end-value {
				break
			}
			value += step
		}
	}
	return strings.Join(normalized, ","), nil
}

func cronMatches(expr string, current time.Time) bool {
	schedule, err := parseCron(expr)
	if err != nil {
		return false
	}
	current = current.Truncate(time.Second)
	return schedule.Next(current.Add(-time.Second)).Equal(current)
}

func cronRunKey(_ string, current time.Time) int64 { return current.Unix() }

func nextCronRuns(expr string, count int, from time.Time) ([]int64, error) {
	schedule, err := parseCron(expr)
	if err != nil {
		return nil, err
	}
	if count < 1 || count > 100 {
		return nil, fmt.Errorf("run count must be between 1 and 100")
	}
	runs := make([]int64, 0, count)
	for len(runs) < count {
		// Jump between matching dates instead of scanning millions of seconds on a router CPU.
		from = schedule.Next(from)
		if from.IsZero() {
			return nil, fmt.Errorf("cron has no future run")
		}
		runs = append(runs, from.UnixMilli())
	}
	return runs, nil
}
