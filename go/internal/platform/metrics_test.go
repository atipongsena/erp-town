package platform

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsHandlerExposesCounterNames(t *testing.T) {
	m, h, err := NewMetrics()
	require.NoError(t, err)
	m.EventsInvalid.Add(t.Context(), 2)
	m.EventsDropped.Add(t.Context(), 1)
	m.OutboxPublishFailures.Add(t.Context(), 3)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/town/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Regexp(t, `(?m)^town_events_invalid_total(\{[^}]*\})? 2$`, body)
	assert.Regexp(t, `(?m)^town_events_dropped_total(\{[^}]*\})? 1$`, body)
	assert.Regexp(t, `(?m)^town_outbox_publish_failures_total(\{[^}]*\})? 3$`, body)
	assert.NotContains(t, body, "_total_total")
}

func TestNewMetricsTwiceDoesNotConflict(t *testing.T) {
	_, _, err := NewMetrics()
	require.NoError(t, err)
	_, _, err = NewMetrics()
	require.NoError(t, err)
}

func TestTestMetricsReadsCounters(t *testing.T) {
	m, read := NewTestMetrics()
	assert.Equal(t, int64(0), read("town_events_invalid_total"))
	m.EventsInvalid.Add(t.Context(), 1)
	m.EventsInvalid.Add(t.Context(), 1)
	m.EventsDropped.Add(t.Context(), 1)
	assert.Equal(t, int64(2), read("town_events_invalid_total"))
	assert.Equal(t, int64(1), read("town_events_dropped_total"))
	assert.Equal(t, int64(0), read("town_outbox_publish_failures_total"))
}
