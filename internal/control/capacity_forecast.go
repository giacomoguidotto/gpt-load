package control

import "math"

const (
	quotaFullBasisPoints = 10_000
	// Pace from recent history needs a span long enough to smooth the
	// provider's percent rounding.
	quotaRecentPaceLookbackMS = 6 * 60 * 60 * 1000
	quotaRecentPaceMinSpanMS  = 20 * 60 * 1000
	// A window-average pace needs some elapsed time to mean anything.
	quotaWindowPaceMinElapsedMS = 10 * 60 * 1000
	// Providers jitter reset timestamps slightly between observations.
	quotaResetMatchToleranceMS = 15 * 60 * 1000
)

// Pace sources, from most to least specific.
const (
	quotaPaceRecent        = "recent"
	quotaPaceWindowAverage = "window_average"
	quotaPaceNone          = "none"
)

// QuotaForecast projects when a quota window runs out at its current pace.
type QuotaForecast struct {
	WindowID string `json:"window_id"`
	// UsedBasisPoints is the observed share of the window already used.
	UsedBasisPoints int64  `json:"used_basis_points"`
	ResetAtMS       *int64 `json:"reset_at_ms"`
	// PaceBasisPointsPerHour is zero when no pace could be measured.
	PaceBasisPointsPerHour float64 `json:"pace_basis_points_per_hour"`
	PaceSource             string  `json:"pace_source"`
	// ExhaustsAtMS is set only when the window runs out before it resets.
	ExhaustsAtMS *int64 `json:"exhausts_at_ms"`
	Exhausted    bool   `json:"exhausted"`
	// ResetPassed means the window has reset since it was last observed, so
	// its usage is no longer known.
	ResetPassed bool `json:"reset_passed"`
}

type quotaHistorySample struct {
	ObservedAtMS    int64
	UsedBasisPoints int64
	ResetAtMS       *int64
}

type quotaForecastInput struct {
	WindowID        string
	UsedBasisPoints int64
	NowMS           int64
	ObservedAtMS    int64
	ResetAtMS       *int64
	WindowSeconds   *int64
	// History holds earlier observations of the same window, in any order.
	History []quotaHistorySample
}

// forecastQuotaWindow estimates the window's consumption pace and the time it
// runs out. Recent history is preferred; without it, the average pace since
// the window opened is used.
func forecastQuotaWindow(input quotaForecastInput) QuotaForecast {
	used := min(max(input.UsedBasisPoints, 0), quotaFullBasisPoints)
	result := QuotaForecast{
		WindowID:        input.WindowID,
		UsedBasisPoints: used,
		ResetAtMS:       input.ResetAtMS,
		PaceSource:      quotaPaceNone,
	}
	if input.ResetAtMS != nil && *input.ResetAtMS <= input.NowMS {
		result.ResetPassed = true
		return result
	}
	if used >= quotaFullBasisPoints {
		result.Exhausted = true
		return result
	}

	if pace, ok := recentQuotaPace(input, used); ok {
		result.PaceBasisPointsPerHour, result.PaceSource = pace, quotaPaceRecent
	} else if pace, ok := windowAverageQuotaPace(input, used); ok {
		result.PaceBasisPointsPerHour, result.PaceSource = pace, quotaPaceWindowAverage
	}
	if result.PaceBasisPointsPerHour <= 0 {
		return result
	}

	remainingHours := float64(quotaFullBasisPoints-used) / result.PaceBasisPointsPerHour
	exhaustsAt := input.ObservedAtMS + int64(math.Ceil(remainingHours*60*60*1000))
	if input.ResetAtMS != nil && exhaustsAt >= *input.ResetAtMS {
		return result
	}
	result.ExhaustsAtMS = &exhaustsAt
	return result
}

// recentQuotaPace measures the change from the oldest observation in the
// lookback that belongs to the current window up to the current observation.
func recentQuotaPace(input quotaForecastInput, used int64) (float64, bool) {
	var earliest *quotaHistorySample
	for index := range input.History {
		sample := &input.History[index]
		if sample.ObservedAtMS >= input.ObservedAtMS ||
			input.ObservedAtMS-sample.ObservedAtMS > quotaRecentPaceLookbackMS ||
			!sameQuotaReset(sample.ResetAtMS, input.ResetAtMS) {
			continue
		}
		if earliest == nil || sample.ObservedAtMS < earliest.ObservedAtMS {
			earliest = sample
		}
	}
	if earliest == nil {
		return 0, false
	}
	span := input.ObservedAtMS - earliest.ObservedAtMS
	if span < quotaRecentPaceMinSpanMS || used < earliest.UsedBasisPoints {
		return 0, false
	}
	return float64(used-earliest.UsedBasisPoints) / (float64(span) / (60 * 60 * 1000)), true
}

func windowAverageQuotaPace(input quotaForecastInput, used int64) (float64, bool) {
	if input.ResetAtMS == nil || input.WindowSeconds == nil || *input.WindowSeconds <= 0 {
		return 0, false
	}
	startedAt := *input.ResetAtMS - *input.WindowSeconds*1000
	elapsed := input.ObservedAtMS - startedAt
	if elapsed < quotaWindowPaceMinElapsedMS {
		return 0, false
	}
	return float64(used) / (float64(elapsed) / (60 * 60 * 1000)), true
}

func sameQuotaReset(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	difference := *left - *right
	return difference <= quotaResetMatchToleranceMS && difference >= -quotaResetMatchToleranceMS
}

// quotaWindowUsedBasisPoints reads a percent-unit window's usage. Other units
// (credits, requests) have no comparable full mark and are not forecast.
func quotaWindowUsedBasisPoints(window ObservationQuotaWindow) (int64, bool) {
	if window.Unit != "percent" {
		return 0, false
	}
	switch {
	case window.Utilization != nil:
		return int64(math.Round(*window.Utilization * quotaFullBasisPoints)), true
	case window.Used != nil && window.Limit != nil && *window.Limit > 0:
		return int64(math.Round(*window.Used / *window.Limit * quotaFullBasisPoints)), true
	default:
		return 0, false
	}
}
