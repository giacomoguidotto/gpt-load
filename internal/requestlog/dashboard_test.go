package requestlog

import "testing"

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []int64{100, 400, 200, 300, 500, 600, 700, 800, 900, 1000}

	if got := percentile(values, 50); got != 500 {
		t.Fatalf("p50 = %d, want 500", got)
	}
	if got := percentile(values, 95); got != 1000 {
		t.Fatalf("p95 = %d, want 1000", got)
	}
}

func TestLatencySummaryOmitsFirstResponseWithoutSamples(t *testing.T) {
	summary := summarizeLatency([]latencyRow{{ClientModel: "m", DurationMs: 50}})

	if summary.FirstResponseP50 != nil || summary.DurationP50MS != 50 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestErrorCausesRankByCount(t *testing.T) {
	causes := errorCauses([]failureRow{
		{Status: "error", ErrorCode: "upstream_rate_limited"},
		{Status: "error", ErrorCode: "upstream_timeout"},
		{Status: "error", ErrorCode: "upstream_rate_limited"},
	})

	if len(causes) != 2 || causes[0].ErrorCode != "upstream_rate_limited" || causes[0].Count != 2 {
		t.Fatalf("causes = %+v", causes)
	}
}
