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
 		{"every minute", "* * * * *", false},
 		{"daily at 8am", "0 8 * * *", false},
 		{"weekdays 9:30", "30 9 * * 1-5", false},
 		{"every 5 minutes", "*/5 * * * *", false},
 		{"monthly on 1st at midnight", "0 0 1 * *", false},
 		{"invalid - too few fields", "0 8 * *", true},
 		{"invalid - too many fields", "0 8 * * * *", true},
 		{"invalid - bad minute", "60 8 * * *", true},
 		{"invalid - bad hour", "0 24 * * *", true},
 	}
 
 	for _, tt := range tests {
 		t.Run(tt.name, func(t *testing.T) {
 			schedule, err := ParseCron(tt.expr)
 			if tt.wantErr {
 				if err == nil {
 					t.Errorf("ParseCron(%q) expected error, got nil", tt.expr)
 				}
 				return
 			}
 			if err != nil {
 				t.Errorf("ParseCron(%q) unexpected error: %v", tt.expr, err)
 				return
 			}
 			if schedule == nil {
 				t.Errorf("ParseCron(%q) returned nil schedule", tt.expr)
 			}
 		})
 	}
 }
 
 func TestScheduleNext(t *testing.T) {
 	tests := []struct {
 		name     string
 		expr     string
 		from     time.Time
 		wantHour int
 		wantMin  int
 	}{
 		{
 			name:     "daily at 8am - next day",
 			expr:     "0 8 * * *",
 			from:     time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
 			wantHour: 8,
 			wantMin:  0,
 		},
 		{
 			name:     "every 5 minutes",
 			expr:     "*/5 * * * *",
 			from:     time.Date(2024, 1, 15, 10, 3, 0, 0, time.UTC),
 			wantHour: 10,
 			wantMin:  5,
 		},
 	}
 
 	for _, tt := range tests {
 		t.Run(tt.name, func(t *testing.T) {
 			schedule, err := ParseCron(tt.expr)
 			if err != nil {
 				t.Fatalf("ParseCron(%q) error: %v", tt.expr, err)
 			}
 
 			next := schedule.Next(tt.from)
 			if next.Hour() != tt.wantHour {
 				t.Errorf("Next().Hour() = %d, want %d", next.Hour(), tt.wantHour)
 			}
 			if next.Minute() != tt.wantMin {
 				t.Errorf("Next().Minute() = %d, want %d", next.Minute(), tt.wantMin)
 			}
 		})
 	}
 }
 
 func TestValidateTask(t *testing.T) {
 	tests := []struct {
 		name    string
 		task    *Task
 		wantErr bool
 	}{
 		{
 			name: "valid task",
 			task: &Task{
 				Name:     "music-recommendation",
 				CronExpr: "0 8 * * *",
 				Prompt:   "Recommend some music",
 				UserID:   1,
 			},
 			wantErr: false,
 		},
 		{
 			name: "missing name",
 			task: &Task{
 				CronExpr: "0 8 * * *",
 				Prompt:   "Recommend some music",
 				UserID:   1,
 			},
 			wantErr: true,
 		},
 		{
 			name: "missing cron",
 			task: &Task{
 				Name:   "music-recommendation",
 				Prompt: "Recommend some music",
 				UserID: 1,
 			},
 			wantErr: true,
 		},
 		{
 			name: "missing prompt",
 			task: &Task{
 				Name:     "music-recommendation",
 				CronExpr: "0 8 * * *",
 				UserID:   1,
 			},
 			wantErr: true,
 		},
 		{
 			name: "missing user ID",
 			task: &Task{
 				Name:     "music-recommendation",
 				CronExpr: "0 8 * * *",
 				Prompt:   "Recommend some music",
 			},
 			wantErr: true,
 		},
 		{
 			name: "invalid cron",
 			task: &Task{
 				Name:     "music-recommendation",
 				CronExpr: "invalid",
 				Prompt:   "Recommend some music",
 				UserID:   1,
 			},
 			wantErr: true,
 		},
 	}
 
 	for _, tt := range tests {
 		t.Run(tt.name, func(t *testing.T) {
 			err := ValidateTask(tt.task)
 			if tt.wantErr && err == nil {
 				t.Error("ValidateTask() expected error, got nil")
 			}
 			if !tt.wantErr && err != nil {
 				t.Errorf("ValidateTask() unexpected error: %v", err)
 			}
 		})
 	}
 }
