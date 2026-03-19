package cron

import (
	"testing"
	"time"
)

func TestParseInterceptDateForCron(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkFunc func(t *testing.T, got time.Time)
	}{
		{
			name:  "datetime format",
			input: "2026-03-19 11:00:01",
			checkFunc: func(t *testing.T, got time.Time) {
				expected := time.Date(2026, 3, 19, 11, 0, 1, 0, time.Local)
				if !got.Equal(expected) {
					t.Errorf("expected %v, got %v", expected, got)
				}
			},
		},
		{
			name:  "short YYYYMMDD — end of day",
			input: "20260319",
			checkFunc: func(t *testing.T, got time.Time) {
				if got.Hour() != 23 || got.Minute() != 59 || got.Second() != 59 {
					t.Errorf("expected 23:59:59, got %s", got.Format("15:04:05"))
				}
			},
		},
		{
			name:  "dash format — end of day",
			input: "2026-03-19",
			checkFunc: func(t *testing.T, got time.Time) {
				if got.Hour() != 23 || got.Minute() != 59 || got.Second() != 59 {
					t.Errorf("expected 23:59:59, got %s", got.Format("15:04:05"))
				}
			},
		},
		{
			name:    "invalid",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInterceptDateForCron(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, got)
			}
		})
	}
}

func TestCronExpiredDetection(t *testing.T) {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.Local)

	// datetime 형식: 11시에 만료 → 12시 now 기준 만료됨
	banEnd, _ := parseInterceptDateForCron("2026-03-19 11:00:01")
	if !now.After(banEnd) {
		t.Error("datetime ban ending at 11:00:01 should be expired at 12:00:00")
	}

	// datetime 형식: 13시에 만료 → 아직 유효
	banEnd2, _ := parseInterceptDateForCron("2026-03-19 13:00:00")
	if now.After(banEnd2) {
		t.Error("datetime ban ending at 13:00:00 should NOT be expired at 12:00:00")
	}

	// YYYYMMDD 형식: 20260319 → 23:59:59까지 유효 → 12시에는 아직 유효
	banEnd3, _ := parseInterceptDateForCron("20260319")
	if now.After(banEnd3) {
		t.Error("YYYYMMDD ban for 20260319 should NOT be expired at 12:00:00 (valid until 23:59:59)")
	}

	// YYYYMMDD 형식: 20260318 → 어제 → 만료됨
	banEnd4, _ := parseInterceptDateForCron("20260318")
	if !now.After(banEnd4) {
		t.Error("YYYYMMDD ban for 20260318 should be expired at 2026-03-19 12:00:00")
	}
}
