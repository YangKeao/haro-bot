 package scheduler
 
 import (
 	"fmt"
 	"strconv"
 	"strings"
 	"time"
 )
 
 // Schedule represents a parsed cron schedule.
 type Schedule struct {
 	minute     field
 	hour       field
 	dayOfMonth field
 	month      field
 	dayOfWeek  field
 }
 
 // field represents a cron field (e.g., minute, hour).
 type field struct {
 	values map[int]bool
 }
 
 // ParseCron parses a cron expression into a Schedule.
 // Supports standard 5-field cron format: minute hour day-of-month month day-of-week
 // Examples:
 //   - "0 8 * * *" - Every day at 8:00 AM
 //   - "30 9 * * 1-5" - Weekdays at 9:30 AM
 //   - "0 0 1 * *" - First day of every month at midnight
 func ParseCron(expr string) (*Schedule, error) {
 	parts := strings.Fields(expr)
 	if len(parts) != 5 {
 		return nil, fmt.Errorf("expected 5 fields, got %d", len(parts))
 	}
 
 	minute, err := parseField(parts[0], 0, 59)
 	if err != nil {
 		return nil, fmt.Errorf("invalid minute field: %w", err)
 	}
 
 	hour, err := parseField(parts[1], 0, 23)
 	if err != nil {
 		return nil, fmt.Errorf("invalid hour field: %w", err)
 	}
 
 	dayOfMonth, err := parseField(parts[2], 1, 31)
 	if err != nil {
 		return nil, fmt.Errorf("invalid day-of-month field: %w", err)
 	}
 
 	month, err := parseField(parts[3], 1, 12)
 	if err != nil {
 		return nil, fmt.Errorf("invalid month field: %w", err)
 	}
 
 	dayOfWeek, err := parseField(parts[4], 0, 6)
 	if err != nil {
 		return nil, fmt.Errorf("invalid day-of-week field: %w", err)
 	}
 
 	return &Schedule{
 		minute:     minute,
 		hour:       hour,
 		dayOfMonth: dayOfMonth,
 		month:      month,
 		dayOfWeek:  dayOfWeek,
 	}, nil
 }
 
 // parseField parses a single cron field.
 func parseField(s string, min, max int) (field, error) {
 	f := field{values: make(map[int]bool)}
 
 	// Handle multiple values separated by comma
 	for _, part := range strings.Split(s, ",") {
 		if err := parseFieldPart(part, min, max, f.values); err != nil {
 			return f, err
 		}
 	}
 
 	return f, nil
 }
 
 // parseFieldPart parses a single part of a cron field.
 func parseFieldPart(s string, min, max int, values map[int]bool) error {
 	// Handle wildcard
 	if s == "*" {
 		for i := min; i <= max; i++ {
 			values[i] = true
 		}
 		return nil
 	}
 
 	// Handle step (e.g., "*/5" or "10-20/2")
 	if strings.Contains(s, "/") {
 		return parseStep(s, min, max, values)
 	}
 
 	// Handle range (e.g., "1-5")
 	if strings.Contains(s, "-") {
 		return parseRange(s, min, max, values)
 	}
 
 	// Handle single value
 	val, err := strconv.Atoi(s)
 	if err != nil {
 		return fmt.Errorf("invalid value %q: %w", s, err)
 	}
 	if val < min || val > max {
 		return fmt.Errorf("value %d out of range [%d, %d]", val, min, max)
 	}
 	values[val] = true
 	return nil
 }
 
 // parseStep parses a step expression (e.g., "*/5" or "10-20/2").
 func parseStep(s string, min, max int, values map[int]bool) error {
 	parts := strings.Split(s, "/")
 	if len(parts) != 2 {
 		return fmt.Errorf("invalid step expression %q", s)
 	}
 
 	step, err := strconv.Atoi(parts[1])
 	if err != nil || step <= 0 {
 		return fmt.Errorf("invalid step value %q", parts[1])
 	}
 
 	rangePart := parts[0]
 	var start, end int
 
 	if rangePart == "*" {
 		start, end = min, max
 	} else if strings.Contains(rangePart, "-") {
 		rangeParts := strings.Split(rangePart, "-")
 		if len(rangeParts) != 2 {
 			return fmt.Errorf("invalid range %q", rangePart)
 		}
 		start, err = strconv.Atoi(rangeParts[0])
 		if err != nil {
 			return fmt.Errorf("invalid range start %q", rangeParts[0])
 		}
 		end, err = strconv.Atoi(rangeParts[1])
 		if err != nil {
 			return fmt.Errorf("invalid range end %q", rangeParts[1])
 		}
 	} else {
 		start, err = strconv.Atoi(rangePart)
 		if err != nil {
 			return fmt.Errorf("invalid value %q", rangePart)
 		}
 		end = max
 	}
 
 	if start < min || end > max {
 		return fmt.Errorf("range [%d, %d] out of bounds [%d, %d]", start, end, min, max)
 	}
 
 	for i := start; i <= end; i += step {
 		values[i] = true
 	}
 	return nil
 }
 
 // parseRange parses a range expression (e.g., "1-5").
 func parseRange(s string, min, max int, values map[int]bool) error {
 	parts := strings.Split(s, "-")
 	if len(parts) != 2 {
 		return fmt.Errorf("invalid range %q", s)
 	}
 
 	start, err := strconv.Atoi(parts[0])
 	if err != nil {
 		return fmt.Errorf("invalid range start %q", parts[0])
 	}
 
 	end, err := strconv.Atoi(parts[1])
 	if err != nil {
 		return fmt.Errorf("invalid range end %q", parts[1])
 	}
 
 	if start > end {
 		return fmt.Errorf("range start %d > end %d", start, end)
 	}
 	if start < min || end > max {
 		return fmt.Errorf("range [%d, %d] out of bounds [%d, %d]", start, end, min, max)
 	}
 
 	for i := start; i <= end; i++ {
 		values[i] = true
 	}
 	return nil
 }
 
 // Next returns the next activation time after the given time.
 func (s *Schedule) Next(t time.Time) time.Time {
 	// Start from the next minute
 	t = t.Add(1*time.Minute - time.Duration(t.Second())*time.Second - time.Duration(t.Nanosecond())*time.Nanosecond)
 
 	// Search for the next matching time, up to 5 years
 	for i := 0; i < 5*366*24*60; i++ {
 		if s.matches(t) {
 			return t
 		}
 		t = t.Add(time.Minute)
 	}
 
 	// No match found within reasonable time
 	return time.Time{}
 }
 
 // matches checks if the schedule matches the given time.
 func (s *Schedule) matches(t time.Time) bool {
 	return s.minute.values[t.Minute()] &&
 		s.hour.values[t.Hour()] &&
 		s.dayOfMonth.values[t.Day()] &&
 		s.month.values[int(t.Month())] &&
 		s.dayOfWeek.values[int(t.Weekday())]
 }