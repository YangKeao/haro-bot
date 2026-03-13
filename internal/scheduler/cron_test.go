package scheduler

import (
	"testing"
	"time"
)

func TestParseCron(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{
			name:    "every minute",
			expr:    "* * * * *",
			wantErr: false,
		},
		{
			name:    "daily at 8am",
			expr:    "0 8 * * *",
			wantErr: false,
		},
		{
			name:    "weekdays 9:30",
			expr:    "30 9 * * 1-5",
			wantErr: false,
		},
		{
			name:    "every 5 minutes",
			expr:    "*/5 * * * *",
			wantErr: false,
		},
		{
			name:    "monthly on 1st at midnight",
			expr:    "0 0 1 * *",
			wantErr: false,
		},
		{
			name:    "invalid - too few fields",
			expr:    "* * * *",
			wantErr: true,
		},
		{
			name:    "invalid - too many fields",
			expr:    "* * * * * *",
			wantErr: true,
		},
		{
			name:    "invalid - bad minute",
			expr:    "60 * * * *",
			wantErr: true,
		},
		{
			name:    "invalid - bad hour",
			expr:    "* 25 * * *",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := ParseCron(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseCron() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("ParseCron() unexpected error: %v", err)
				}
				if schedule == nil {
					t.Error("ParseCron() returned nil schedule")
				}
			}
		})
	}
}

func TestScheduleNext(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		from    time.Time
		wantMin int
	}{
		{
			name:    "daily at 8am - next day",
			expr:    "0 8 * * *",
			from:    time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
			wantMin: 0,
		},
		{
			name:    "every 5 minutes",
			expr:    "*/5 * * * *",
			from:    time.Date(2024, 1, 15, 9, 3, 0, 0, time.UTC),
			wantMin: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := ParseCron(tt.expr)
			if err != nil {
				t.Fatalf("ParseCron() error: %v", err)
			}
			next := schedule.Next(tt.from)
			if next.Minute() != tt.wantMin {
				t.Errorf("Next().Minute() = %d, want %d", next.Minute(), tt.wantMin)
			}
		})
	}
}
