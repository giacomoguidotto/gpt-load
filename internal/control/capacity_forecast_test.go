package control

import "testing"

const hourMS = int64(60 * 60 * 1000)

func TestQuotaForecastProjectsExhaustionFromRecentPace(t *testing.T) {
	now := int64(1_000) * hourMS
	reset := int64Pointer(now + 48*hourMS)
	forecast := forecastQuotaWindow(quotaForecastInput{
		UsedBasisPoints: 6_000, ObservedAtMS: now, ResetAtMS: reset,
		History: []quotaHistorySample{{ObservedAtMS: now - 2*hourMS, UsedBasisPoints: 5_000, ResetAtMS: reset}},
	})

	// 1,000 bp in 2 h is 500 bp/h; the remaining 4,000 bp last 8 h.
	if forecast.PaceSource != quotaPaceRecent || forecast.ExhaustsAtMS == nil || *forecast.ExhaustsAtMS != now+8*hourMS {
		t.Fatalf("forecast = %+v, want recent pace running out at +8h", forecast)
	}
}

func TestQuotaForecastLastsUntilResetWhenPaceIsSlow(t *testing.T) {
	now := int64(1_000) * hourMS
	reset := int64Pointer(now + 2*hourMS)
	forecast := forecastQuotaWindow(quotaForecastInput{
		UsedBasisPoints: 6_000, ObservedAtMS: now, ResetAtMS: reset,
		History: []quotaHistorySample{{ObservedAtMS: now - 2*hourMS, UsedBasisPoints: 5_000, ResetAtMS: reset}},
	})

	if forecast.ExhaustsAtMS != nil || forecast.Exhausted {
		t.Fatalf("forecast = %+v, want the window to outlast its reset", forecast)
	}
}

func TestQuotaForecastIgnoresHistoryFromThePreviousWindow(t *testing.T) {
	now := int64(1_000) * hourMS
	reset := int64Pointer(now + 100*hourMS)
	windowSeconds := int64Pointer(168 * 60 * 60)
	forecast := forecastQuotaWindow(quotaForecastInput{
		UsedBasisPoints: 1_000, ObservedAtMS: now, ResetAtMS: reset, WindowSeconds: windowSeconds,
		// A sample from before the last reset must not count as a decrease.
		History: []quotaHistorySample{{ObservedAtMS: now - 3*hourMS, UsedBasisPoints: 9_000, ResetAtMS: int64Pointer(now - 68*hourMS)}},
	})

	// The window opened 68 h ago; 1,000 bp over 68 h.
	if forecast.PaceSource != quotaPaceWindowAverage {
		t.Fatalf("pace source = %q, want window average", forecast.PaceSource)
	}
}

func TestQuotaForecastReportsAnExhaustedWindow(t *testing.T) {
	forecast := forecastQuotaWindow(quotaForecastInput{UsedBasisPoints: 10_000, ObservedAtMS: hourMS})

	if !forecast.Exhausted || forecast.ExhaustsAtMS != nil {
		t.Fatalf("forecast = %+v, want exhausted", forecast)
	}
}

func TestQuotaForecastHasNoPaceWithoutEvidence(t *testing.T) {
	forecast := forecastQuotaWindow(quotaForecastInput{UsedBasisPoints: 3_000, ObservedAtMS: hourMS})

	if forecast.PaceSource != quotaPaceNone || forecast.ExhaustsAtMS != nil {
		t.Fatalf("forecast = %+v, want no pace", forecast)
	}
}

func TestQuotaForecastMarksAWindowThatResetSinceItWasObserved(t *testing.T) {
	now := int64(1_000) * hourMS
	forecast := forecastQuotaWindow(quotaForecastInput{
		UsedBasisPoints: 10_000, NowMS: now, ObservedAtMS: now - 3*hourMS, ResetAtMS: int64Pointer(now - hourMS),
	})

	if !forecast.ResetPassed || forecast.Exhausted {
		t.Fatalf("forecast = %+v, want reset passed and not exhausted", forecast)
	}
}
