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
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser accepts a five-field cron expression: minute, hour, day of month, month, day of week.
// Descriptors such as @daily are rejected so the function matches the BackupPolicy schedule field.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// nextRun returns when the next backup run is scheduled.
// last is the time of the previous successful run. The zero time means there has not been one.
// Times are read and returned in UTC.
// The result is the earliest cron instant strictly after last.
// When last is zero, or last is after now, that instant is taken from now.
// A result at or before now means a run is due.
func nextRun(schedule string, last, now time.Time) (time.Time, error) {
	if now.IsZero() {
		return time.Time{}, fmt.Errorf("now must be set")
	}

	parsed, err := cronParser.Parse(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse schedule %q: %w", schedule, err)
	}

	from := now.UTC()
	if !last.IsZero() && !last.After(now) {
		from = last.UTC()
	}

	next := parsed.Next(from)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("schedule %q has no upcoming time", schedule)
	}
	return next, nil
}
