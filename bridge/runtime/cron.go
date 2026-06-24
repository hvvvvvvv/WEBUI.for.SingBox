package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronField struct {
	allowed map[int]bool
}

func parseCronField(expr string, minValue int, maxValue int) (cronField, error) {
	field := cronField{allowed: map[int]bool{}}
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		step := 1
		if strings.Contains(part, "/") {
			pieces := strings.SplitN(part, "/", 2)
			part = pieces[0]
			value, err := strconv.Atoi(pieces[1])
			if err != nil || value <= 0 {
				return field, fmt.Errorf("invalid cron step %q", pieces[1])
			}
			step = value
		}
		start, end := minValue, maxValue
		if part != "*" && part != "?" {
			if strings.Contains(part, "-") {
				pieces := strings.SplitN(part, "-", 2)
				rangeStart, err := strconv.Atoi(pieces[0])
				if err != nil {
					return field, err
				}
				rangeEnd, err := strconv.Atoi(pieces[1])
				if err != nil {
					return field, err
				}
				start, end = rangeStart, rangeEnd
			} else {
				value, err := strconv.Atoi(part)
				if err != nil {
					return field, err
				}
				start, end = value, value
			}
		}
		if start < minValue || end > maxValue || start > end {
			return field, fmt.Errorf("cron value out of range: %s", expr)
		}
		for value := start; value <= end; value += step {
			field.allowed[value] = true
		}
	}
	return field, nil
}

func (f cronField) match(value int) bool {
	return f.allowed[value]
}

func parseCron(expr string) ([]cronField, error) {
	parts := strings.Fields(expr)
	if len(parts) != 6 {
		return nil, fmt.Errorf("cron must have 6 fields")
	}
	ranges := [][2]int{{0, 59}, {0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	fields := make([]cronField, 6)
	for index, part := range parts {
		field, err := parseCronField(part, ranges[index][0], ranges[index][1])
		if err != nil {
			return nil, err
		}
		fields[index] = field
	}
	return fields, nil
}

func cronMatches(expr string, current time.Time) bool {
	fields, err := parseCron(expr)
	if err != nil {
		return false
	}
	weekday := int(current.Weekday())
	return fields[0].match(current.Second()) &&
		fields[1].match(current.Minute()) &&
		fields[2].match(current.Hour()) &&
		fields[3].match(current.Day()) &&
		fields[4].match(int(current.Month())) &&
		(fields[5].match(weekday) || (weekday == 0 && fields[5].match(7)))
}

func cronRunKey(expr string, current time.Time) int64 {
	parts := strings.Fields(expr)
	if len(parts) == 6 && (parts[0] == "*" || parts[0] == "?") {
		return current.Truncate(time.Minute).Unix()
	}
	return current.Unix()
}

func nextCronRuns(expr string, count int, from time.Time) ([]int64, error) {
	if _, err := parseCron(expr); err != nil {
		return nil, err
	}
	var runs []int64
	seen := map[int64]bool{}
	cursor := from.Truncate(time.Second).Add(time.Second)
	deadline := cursor.AddDate(1, 0, 0)
	for len(runs) < count && cursor.Before(deadline) {
		if cronMatches(expr, cursor) {
			runKey := cronRunKey(expr, cursor)
			if seen[runKey] {
				cursor = cursor.Add(time.Second)
				continue
			}
			seen[runKey] = true
			runs = append(runs, cursor.UnixMilli())
		}
		cursor = cursor.Add(time.Second)
	}
	if len(runs) == 0 {
		return nil, fmt.Errorf("no runs found within one year")
	}
	return runs, nil
}
