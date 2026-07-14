package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule computes run times for At services. Next returns the first run
// time strictly after the given moment, or the zero time when the schedule
// will never fire again.
type Schedule interface {
	Next(after time.Time) time.Time
}

// ScheduleFunc adapts a function to the Schedule interface.
type ScheduleFunc func(after time.Time) time.Time

func (f ScheduleFunc) Next(after time.Time) time.Time { return f(after) }

// Expr parses a standard 5-field cron expression:
//
//	┌──────────── minute       (0-59)
//	│ ┌────────── hour         (0-23)
//	│ │ ┌──────── day of month (1-31)
//	│ │ │ ┌────── month        (1-12 or jan-dec)
//	│ │ │ │ ┌──── day of week  (0-6 or sun-sat; 7 = sunday)
//	│ │ │ │ │
//	* * * * *
//
// Each field accepts wildcards (*), lists (1,15), ranges (9-17) and steps
// (*/5, 9-17/2). The aliases @hourly, @daily (@midnight), @weekly,
// @monthly and @yearly (@annually) are supported. As in classic cron,
// when both day-of-month and day-of-week are restricted the job runs when
// either matches. Times are evaluated in the location of the time passed
// to Next (the host's local time when used with At).
func Expr(spec string) (Schedule, error) {
	if alias, ok := aliases[strings.ToLower(strings.TrimSpace(spec))]; ok {
		spec = alias
	}
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: expression must have 5 fields, got %d in %q", len(fields), spec)
	}
	var (
		s   exprSchedule
		err error
	)
	if s.min, err = parseField(fields[0], 0, 59, nil); err != nil {
		return nil, fmt.Errorf("cron: minute field: %w", err)
	}
	if s.hour, err = parseField(fields[1], 0, 23, nil); err != nil {
		return nil, fmt.Errorf("cron: hour field: %w", err)
	}
	if s.dom, err = parseField(fields[2], 1, 31, nil); err != nil {
		return nil, fmt.Errorf("cron: day-of-month field: %w", err)
	}
	if s.month, err = parseField(fields[3], 1, 12, monthNames); err != nil {
		return nil, fmt.Errorf("cron: month field: %w", err)
	}
	if s.dow, err = parseField(fields[4], 0, 7, dowNames); err != nil {
		return nil, fmt.Errorf("cron: day-of-week field: %w", err)
	}
	// 7 is an alias for sunday.
	if s.dow&(1<<7) != 0 {
		s.dow |= 1 << 0
	}
	s.domStar = fields[2] == "*"
	s.dowStar = fields[4] == "*"
	return &s, nil
}

// MustExpr is Expr panicking on a malformed expression. Intended for
// literal expressions in main().
func MustExpr(spec string) Schedule {
	s, err := Expr(spec)
	if err != nil {
		panic(err)
	}
	return s
}

var aliases = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var dowNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// exprSchedule holds one bit per allowed value of each field.
type exprSchedule struct {
	min, hour, dom, month, dow uint64
	domStar, dowStar           bool
}

func parseField(field string, min, max int, names map[string]int) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(field, ",") {
		b, err := parsePart(part, min, max, names)
		if err != nil {
			return 0, err
		}
		bits |= b
	}
	return bits, nil
}

func parsePart(part string, min, max int, names map[string]int) (uint64, error) {
	if part == "" {
		return 0, fmt.Errorf("empty value")
	}
	expr, stepStr, hasStep := strings.Cut(part, "/")
	step := 1
	if hasStep {
		var err error
		if step, err = strconv.Atoi(stepStr); err != nil || step <= 0 {
			return 0, fmt.Errorf("invalid step %q", stepStr)
		}
	}

	lo, hi := min, max
	switch {
	case expr == "*":
		// full range
	case strings.Contains(expr, "-"):
		loStr, hiStr, _ := strings.Cut(expr, "-")
		var err error
		if lo, err = parseValue(loStr, min, max, names); err != nil {
			return 0, err
		}
		if hi, err = parseValue(hiStr, min, max, names); err != nil {
			return 0, err
		}
		if hi < lo {
			return 0, fmt.Errorf("descending range %q", expr)
		}
	default:
		v, err := parseValue(expr, min, max, names)
		if err != nil {
			return 0, err
		}
		lo = v
		if hasStep {
			hi = max // "5/15" means "5-max/15", as in classic cron
		} else {
			hi = v
		}
	}

	var bits uint64
	for v := lo; v <= hi; v += step {
		bits |= 1 << uint(v)
	}
	return bits, nil
}

func parseValue(s string, min, max int, names map[string]int) (int, error) {
	if names != nil {
		if v, ok := names[strings.ToLower(s)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", s)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("value %d out of range %d-%d", v, min, max)
	}
	return v, nil
}

func (s *exprSchedule) Next(after time.Time) time.Time {
	loc := after.Location()
	t := after.Truncate(time.Minute).Add(time.Minute)
	// An expression may never match (e.g. "0 0 30 2 *"); bound the search.
	limit := after.AddDate(5, 0, 0)

	for t.Before(limit) {
		if s.month&(1<<uint(t.Month())) == 0 {
			// First minute of the next month.
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
			continue
		}
		if s.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc).Add(time.Hour)
			continue
		}
		if s.min&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

// dayMatches applies the classic cron rule: when both day fields are
// restricted the day matches if either one does.
func (s *exprSchedule) dayMatches(t time.Time) bool {
	domOK := s.dom&(1<<uint(t.Day())) != 0
	dowOK := s.dow&(1<<uint(t.Weekday())) != 0
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dowOK
	case s.dowStar:
		return domOK
	default:
		return domOK || dowOK
	}
}
