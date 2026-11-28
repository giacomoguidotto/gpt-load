package control

import (
	"context"
	"net/url"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/platform/epochms"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/response"
	"gpt-load/internal/requestlog"
)

// dashboardMaxWindowMS matches the longest range the overview offers.
const dashboardMaxWindowMS = 7 * epochms.MillisecondsPerDay

// DashboardReader reads latency and failure detail for the overview. The
// request-log service implements it alongside HomeStatisticsReader.
type DashboardReader interface {
	QueryDashboardPerformance(context.Context, requestlog.DashboardQuery) (requestlog.DashboardPerformance, error)
}

type dashboardResponse struct {
	FromMS       int64 `json:"from_ms"`
	ToMS         int64 `json:"to_ms"`
	ObservedAtMS int64 `json:"observed_at_ms"`
	requestlog.DashboardPerformance
}

func (s *Service) QueryDashboardPerformance(
	ctx context.Context,
	query requestlog.DashboardQuery,
) (requestlog.DashboardPerformance, error) {
	if s == nil {
		return requestlog.DashboardPerformance{}, app_errors.ErrInternalServer
	}
	reader, ok := s.homeStatistics.(DashboardReader)
	if !ok {
		return requestlog.DashboardPerformance{}, app_errors.ErrInternalServer
	}
	result, err := reader.QueryDashboardPerformance(ctx, query)
	if err != nil {
		return requestlog.DashboardPerformance{}, app_errors.ParseDBError(err)
	}
	return result, nil
}

func (s *Server) handleDashboard(c *gin.Context) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(values) != 2 || len(values["from_ms"]) != 1 || len(values["to_ms"]) != 1 {
		writeServiceError(c, "dashboard", app_errors.ErrBadRequest)
		return
	}
	window, apiErr := parseUsageQuery(c.Request.URL.RawQuery)
	if apiErr != nil || window.ToMS-window.FromMS > dashboardMaxWindowMS {
		writeServiceError(c, "dashboard", app_errors.ErrBadRequest)
		return
	}
	observedAtMS, err := safeEpochMilliseconds(s.now())
	if err != nil {
		writeServiceError(c, "dashboard", err)
		return
	}
	query := requestlog.DashboardQuery{FromMS: window.FromMS, ToMS: window.ToMS}
	// An access-key session sees only its own traffic.
	if accessKeyID, scoped := currentAccessKeyID(c); scoped {
		query.AccessKeyID = &accessKeyID
	}
	result, err := s.service.QueryDashboardPerformance(c.Request.Context(), query)
	if err != nil {
		writeServiceError(c, "dashboard", err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.SuccessI18n(c, "common.success", dashboardResponse{
		FromMS: window.FromMS, ToMS: window.ToMS, ObservedAtMS: observedAtMS, DashboardPerformance: result,
	})
}
