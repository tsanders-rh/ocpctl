package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/api"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TestReportHandler_GetUsageReport_BadRange verifies the date-range validation,
// which runs before any store access (so no database is required).
func TestReportHandler_GetUsageReport_BadRange(t *testing.T) {
	handler := api.NewReportHandler(nil, nil)

	cases := []struct {
		name  string
		query string
	}{
		{"malformed start_date", "start_date=not-a-date&end_date=2026-01-31"},
		{"malformed end_date", "start_date=2026-01-01&end_date=nope"},
		{"start after end", "start_date=2026-02-01&end_date=2026-01-01"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/admin/reports/usage?"+tc.query, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.GetUsageReport(c)
			require.NoError(t, err) // handler writes JSON and returns nil
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

// TestReportHandler_GetUsageReport_HappyPath is an integration test exercising
// the full aggregation path against a real database. It is skipped until a test
// database harness is configured (see setupTestDB).
func TestReportHandler_GetUsageReport_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	pool := setupTestDB(t)
	defer pool.Close()
	s := store.New(pool)
	handler := api.NewReportHandler(s, nil)

	// Seed a couple of clusters active within the window.
	user := createTestUser(t, s, "reporter@example.com", types.RoleUser)
	createTestCluster(t, s, user.ID, "engineering")
	createTestCluster(t, s, user.ID, "engineering")

	end := time.Now().UTC()
	start := end.AddDate(0, 0, -30)

	e := echo.New()
	url := "/admin/reports/usage?start_date=" + start.Format("2006-01-02") +
		"&end_date=" + end.Format("2006-01-02")
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	setAuthContext(c, createTestUser(t, s, "admin@example.com", types.RoleAdmin))

	require.NoError(t, handler.GetUsageReport(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var report types.UsageReport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))

	assert.Equal(t, start.Format("2006-01-02"), report.StartDate)
	assert.Equal(t, end.Format("2006-01-02"), report.EndDate)
	assert.GreaterOrEqual(t, report.Cost.ClustersActive, 2)
	assert.NotNil(t, report.Lifecycle.ByPlatform)
	assert.NotEmpty(t, report.Profiles)
}
