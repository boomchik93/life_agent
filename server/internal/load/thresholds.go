package load

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Thresholds are the limits of PLAN.md section 14. Stored in settings under
// the key "load_thresholds"; a stored object overrides only the fields it names.
type Thresholds struct {
	SleepYellowBelow float64 `json:"sleep_yellow_below"`
	SleepRedBelow    float64 `json:"sleep_red_below"`

	StudyDayYellowAbove float64 `json:"study_day_yellow_above"`
	StudyDayRedAbove    float64 `json:"study_day_red_above"`

	StudyStreakYellowAbove float64 `json:"study_streak_yellow_above"`
	StudyStreakRedAbove    float64 `json:"study_streak_red_above"`
	// A pause shorter than this does not break a study streak: it matches the
	// 15-minute break the week planner leaves between study chunks.
	BreakMin int `json:"break_min"`

	StudyWeekYellowAbove float64 `json:"study_week_yellow_above"`
	StudyWeekRedAbove    float64 `json:"study_week_red_above"`

	RestWeekYellowBelow float64 `json:"rest_week_yellow_below"`
	RestWeekRedBelow    float64 `json:"rest_week_red_below"`

	FreeEveningsMin  int `json:"free_evenings_min"`
	EveningStartHour int `json:"evening_start_hour"`

	// State is the mean of energy and mood over StateDays days.
	StateYellowAtMost float64 `json:"state_yellow_at_most"`
	StateRedAtMost    float64 `json:"state_red_at_most"`
	StateDays         int     `json:"state_days"`
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		SleepYellowBelow: 6.5, SleepRedBelow: 5.5,
		StudyDayYellowAbove: 6, StudyDayRedAbove: 9,
		StudyStreakYellowAbove: 3, StudyStreakRedAbove: 5, BreakMin: 15,
		StudyWeekYellowAbove: 35, StudyWeekRedAbove: 50,
		RestWeekYellowBelow: 8, RestWeekRedBelow: 4,
		FreeEveningsMin: 2, EveningStartHour: 18,
		StateYellowAtMost: 3, StateRedAtMost: 2, StateDays: 3,
	}
}

// ParseThresholds applies a JSON object on top of the defaults and validates the result.
func ParseThresholds(raw []byte) (Thresholds, error) {
	t := DefaultThresholds()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return Thresholds{}, err
	}
	return t, t.Validate()
}

func (t Thresholds) Validate() error {
	switch {
	case t.SleepRedBelow < 0 || t.SleepRedBelow > t.SleepYellowBelow || t.SleepYellowBelow > 24:
		return errors.New("sleep: need 0 <= red_below <= yellow_below <= 24")
	case t.StudyDayYellowAbove < 0 || t.StudyDayYellowAbove > t.StudyDayRedAbove || t.StudyDayRedAbove > 24:
		return errors.New("study_day: need 0 <= yellow_above <= red_above <= 24")
	case t.StudyStreakYellowAbove < 0 || t.StudyStreakYellowAbove > t.StudyStreakRedAbove:
		return errors.New("study_streak: need 0 <= yellow_above <= red_above")
	case t.StudyWeekYellowAbove < 0 || t.StudyWeekYellowAbove > t.StudyWeekRedAbove:
		return errors.New("study_week: need 0 <= yellow_above <= red_above")
	case t.RestWeekRedBelow < 0 || t.RestWeekRedBelow > t.RestWeekYellowBelow:
		return errors.New("rest_week: need 0 <= red_below <= yellow_below")
	case t.StateRedAtMost < 0 || t.StateRedAtMost > t.StateYellowAtMost || t.StateYellowAtMost > 5:
		return errors.New("state: need 0 <= red_at_most <= yellow_at_most <= 5")
	case t.BreakMin < 0:
		return errors.New("break_min must not be negative")
	case t.FreeEveningsMin < 0 || t.FreeEveningsMin > 7:
		return errors.New("free_evenings_min must be 0..7")
	case t.EveningStartHour < 0 || t.EveningStartHour > 23:
		return fmt.Errorf("evening_start_hour must be 0..23")
	case t.StateDays < 1 || t.StateDays > 30:
		return errors.New("state_days must be 1..30")
	}
	return nil
}
