/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"testing"
	"time"
)

func TestNextRun(t *testing.T) {
	utcPlusOne := time.FixedZone("UTC+1", 60*60)
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}

	sundayAfternoon := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		schedule string
		last     time.Time
		now      time.Time
		want     time.Time
		wantErr  bool
	}{
		{
			name:     "first run waits until the next daily midnight",
			schedule: testSchedule,
			now:      sundayAfternoon,
			want:     time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "exact boundary is excluded when there is no previous run",
			schedule: testSchedule,
			now:      time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC),
			want:     time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "one second before midnight is due at midnight",
			schedule: testSchedule,
			now:      time.Date(2026, 10, 18, 23, 59, 59, 0, time.UTC),
			want:     time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "one second after midnight waits until the next day",
			schedule: testSchedule,
			now:      time.Date(2026, 10, 18, 0, 0, 1, 0, time.UTC),
			want:     time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "previous run exactly on the boundary advances one interval",
			schedule: testSchedule,
			last:     time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC),
			now:      sundayAfternoon,
			want:     time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "missed interval stays in the past",
			schedule: testSchedule,
			last:     time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
			now:      sundayAfternoon,
			want:     time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "last successful time in the future is ignored",
			schedule: testSchedule,
			last:     time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
			now:      sundayAfternoon,
			want:     time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "non-UTC times are evaluated in UTC",
			schedule: testSchedule,
			last:     time.Date(2026, 10, 18, 1, 0, 0, 0, utcPlusOne),
			now:      time.Date(2026, 10, 18, 16, 0, 0, 0, utcPlusOne),
			want:     time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "daylight saving wall clock is converted to UTC first",
			schedule: testSchedule,
			now:      time.Date(2026, 3, 8, 2, 30, 0, 0, newYork),
			want:     time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "leap day moves to the next leap year",
			schedule: "0 0 29 2 *",
			last:     time.Date(2023, 2, 28, 0, 0, 0, 0, time.UTC),
			now:      time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC),
			want:     time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "day 31 skips shorter months",
			schedule: "0 0 31 * *",
			last:     time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
			now:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			want:     time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "year boundary",
			schedule: "0 0 1 1 *",
			last:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			now:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			want:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "weekday schedule",
			schedule: "0 9 * * 1",
			now:      sundayAfternoon,
			want:     time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC),
		},
		{
			name:     "step schedule on an exact step waits for the next step",
			schedule: "*/15 * * * *",
			now:      time.Date(2026, 10, 18, 15, 30, 0, 0, time.UTC),
			want:     time.Date(2026, 10, 18, 15, 45, 0, 0, time.UTC),
		},
		{
			name:     "step schedule from the previous step includes the current boundary",
			schedule: "*/15 * * * *",
			last:     time.Date(2026, 10, 18, 15, 15, 0, 0, time.UTC),
			now:      time.Date(2026, 10, 18, 15, 30, 0, 0, time.UTC),
			want:     time.Date(2026, 10, 18, 15, 30, 0, 0, time.UTC),
		},
		{name: "empty schedule", schedule: "", now: sundayAfternoon, wantErr: true},
		{name: "non-cron text", schedule: "not-a-cron", now: sundayAfternoon, wantErr: true},
		{name: "minute out of range", schedule: "60 0 * * *", now: sundayAfternoon, wantErr: true},
		{name: "hour out of range", schedule: "0 24 * * *", now: sundayAfternoon, wantErr: true},
		{name: "day of month out of range", schedule: "0 0 32 * *", now: sundayAfternoon, wantErr: true},
		{name: "month out of range", schedule: "0 0 * 13 *", now: sundayAfternoon, wantErr: true},
		{name: "day of week out of range", schedule: "0 0 * * 8", now: sundayAfternoon, wantErr: true},
		{name: "too few fields", schedule: "* * * *", now: sundayAfternoon, wantErr: true},
		{name: "too many fields", schedule: "* * * * * *", now: sundayAfternoon, wantErr: true},
		{name: "zero step", schedule: "*/0 * * * *", now: sundayAfternoon, wantErr: true},
		{name: "descriptor", schedule: "@daily", now: sundayAfternoon, wantErr: true},
		{name: "impossible day", schedule: "0 0 31 2 *", now: sundayAfternoon, wantErr: true},
		{name: "zero now", schedule: testSchedule, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nextRun(tt.schedule, tt.last, tt.now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("nextRun() error = nil, want an error")
				}
				if !got.IsZero() {
					t.Fatalf("nextRun() = %s, want the zero time", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("nextRun() unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("nextRun() = %s, want %s", got.Format(time.RFC3339), tt.want.Format(time.RFC3339))
			}
			if got.Location() != time.UTC {
				t.Fatalf("nextRun() location = %s, want UTC", got.Location())
			}
		})
	}
}
