//go:build integration

// Package e2e proves the whole sales path on test.localhost: Go gateway → ERPNext →
// the Frappe outbox → the redis-town stream → a Go consumer.
package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp/erpnext"
	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/atipongsena/erp-town/go/internal/gateway"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
)

// Run only through `pnpm go:test:integration`: it seeds test.localhost around the whole
// run, starts the site's scheduler (the outbox publisher) and exports the env below.

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, storetest.IgnoreBackgroundGoroutines()...)
}

const (
	defaultSeedPassword = "town-dev-123"
	salesUser           = "sales@acme-th.test"
	submittedType       = "erp.sales_order.submitted"
	eventWait           = 15 * time.Second
)

type env struct{ erpURL, site, stream, redisURL string }

func testEnv(t *testing.T) env {
	t.Helper()
	e := env{
		erpURL:   os.Getenv("TOWN_TEST_ERP_URL"),
		site:     os.Getenv("TOWN_TEST_ERP_SITE"),
		stream:   os.Getenv("TOWN_TEST_STREAM"),
		redisURL: os.Getenv("TOWN_TEST_REDIS_URL"),
	}
	if e.erpURL == "" || e.site == "" || e.stream == "" || e.redisURL == "" {
		t.Skip("TOWN_TEST_ERP_URL/TOWN_TEST_ERP_SITE/TOWN_TEST_STREAM/TOWN_TEST_REDIS_URL not set: run pnpm go:test:integration")
	}
	return e
}

// login signs in with the seeded password and returns the session id.
func (e env) login(t *testing.T, client *http.Client, user string) string {
	t.Helper()
	pwd := os.Getenv("SEED_USER_PASSWORD")
	if pwd == "" {
		pwd = defaultSeedPassword
	}
	body, err := json.Marshal(map[string]string{"usr": user, "pwd": pwd})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.erpURL+"/api/method/login", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Frappe-Site-Name", e.site)
	res, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusOK, res.StatusCode, "login %s", user)
	for _, c := range res.Cookies() {
		if c.Name == "sid" && c.Value != "" && c.Value != "Guest" {
			return c.Value
		}
	}
	t.Fatalf("login %s: no sid cookie", user)
	return ""
}

// bangkokToday is the site's date (Asia/Bangkok): seeded prices are valid from the seed day.
func bangkokToday() string {
	return time.Now().In(time.FixedZone("ICT", 7*60*60)).Format(time.DateOnly)
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}

// newGateway serves the gateway over the real ERPNext adapter on an httptest server.
func newGateway(t *testing.T, e env) *httptest.Server {
	t.Helper()
	port := erpnext.NewAdapter(erpnext.NewClient(erpnext.ClientConfig{BaseURL: e.erpURL, Site: e.site}))
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	require.NoError(t, err)
	jwt, err := auth.NewJWTIssuer(base64.StdEncoding.EncodeToString(seed), 15*time.Minute)
	require.NoError(t, err)
	mux := platform.NewMux()
	gateway.New(mux, port, auth.NewAuthenticator(port, 30*time.Second), jwt)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// post sends a state-changing gateway request as the session sid and decodes the JSON body.
func post(t *testing.T, srv *httptest.Server, sid, path, body string, wantStatus int, out any) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+path, rd)
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(gateway.TownRequestHeader, "1")
	req.AddCookie(&http.Cookie{Name: "sid", Value: sid})
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, wantStatus, res.StatusCode, "POST %s: %s", path, raw)
	require.NoError(t, json.Unmarshal(raw, out), "POST %s: %s", path, raw)
}

type received struct {
	env events.Envelope
	at  time.Time
}

// startConsumer creates group e2e-<rand> at "$" on the stream, then runs a consumer whose
// handler forwards the submitted event of the Sales Order named by want() (set later) and
// ignores everything else. Cleanup stops the consumer, waits for Run and destroys the group.
func startConsumer(t *testing.T, e env, want func() string) (<-chan received, *redis.Client) {
	t.Helper()
	opts, err := redis.ParseURL(e.redisURL)
	require.NoError(t, err)
	rdb := redis.NewClient(opts)
	t.Cleanup(func() {
		if err := rdb.Close(); err != nil {
			t.Errorf("close redis client: %v", err)
		}
	})

	group := "e2e-" + randomHex(t, 6)
	require.NoError(t, rdb.XGroupCreateMkStream(t.Context(), e.stream, group, "$").Err())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := rdb.XGroupDestroy(ctx, e.stream, group).Err(); err != nil {
			t.Errorf("destroy group %s: %v", group, err)
		}
	})

	pool := storetest.NewPool(t)
	v, err := events.NewValidator()
	require.NoError(t, err)
	m, _ := platform.NewTestMetrics()

	got := make(chan received, 1)
	handler := func(_ context.Context, _ pgx.Tx, ev events.Envelope) error {
		if ev.Type != submittedType || ev.Subject.DocType != "Sales Order" || ev.Subject.Name != want() {
			return nil // another event on the shared stream
		}
		select {
		case got <- received{env: ev, at: time.Now()}:
		default:
		}
		return nil
	}
	c := events.NewConsumer(rdb, pool, v, m, events.DefaultConsumerConfig(e.stream, group, "e2e"), handler)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("consumer run: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("consumer did not stop within 30 s")
		}
	})
	return got, rdb
}

// publishedAt returns the time the stream entry carrying event id was added (the
// millisecond part of its entry id, set by Redis at XADD).
func publishedAt(ctx context.Context, rdb *redis.Client, stream, id string) (time.Time, error) {
	msgs, err := rdb.XRevRangeN(ctx, stream, "+", "-", 1000).Result()
	if err != nil {
		return time.Time{}, err
	}
	for _, msg := range msgs {
		raw, _ := msg.Values["event"].(string)
		var probe struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(raw), &probe) != nil || probe.ID != id {
			continue
		}
		ms, err := strconv.ParseInt(strings.SplitN(msg.ID, "-", 2)[0], 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("entry id %s: %w", msg.ID, err)
		}
		return time.UnixMilli(ms), nil
	}
	return time.Time{}, errors.New("entry not found in the last 1000 stream entries")
}

func TestGatewayToStreamToConsumer(t *testing.T) {
	e := testEnv(t)
	srv := newGateway(t, e)

	loginClient := &http.Client{Transport: &http.Transport{}}
	t.Cleanup(loginClient.CloseIdleConnections)
	sid := e.login(t, loginClient, salesUser)

	var name atomic.Value // the order's name once it exists; read on the consumer goroutine
	name.Store("")
	got, rdb := startConsumer(t, e, func() string { return name.Load().(string) })

	today := bangkokToday()
	order := fmt.Sprintf(`{"company":"ACME-TH","customer":"CUST-001","transaction_date":%q,`+
		`"delivery_date":%q,"lines":[{"item_code":"ITEM-002","qty":"2"}]}`, today, today)
	var created struct {
		Name string `json:"name"`
	}
	post(t, srv, sid, "/town/api/sales-orders", order, http.StatusCreated, &created)
	require.NotEmpty(t, created.Name)
	name.Store(created.Name)

	var submitted struct {
		Submitted bool `json:"submitted"`
	}
	post(t, srv, sid, "/town/api/documents/sales-order/"+created.Name+"/submit", "", http.StatusOK, &submitted)
	require.True(t, submitted.Submitted)

	var r received
	select {
	case r = <-got:
	case <-time.After(eventWait):
		t.Fatalf("no %s for Sales Order %s within %s", submittedType, created.Name, eventWait)
	}

	ev := r.env
	assert.Equal(t, submittedType, ev.Type)
	assert.Equal(t, events.Subject{DocType: "Sales Order", Name: created.Name}, ev.Subject)
	assert.Equal(t, "user", ev.Actor.Kind)
	assert.Equal(t, "user."+salesUser, ev.Actor.ID)
	assert.Nil(t, ev.Actor.OnBehalfOf)
	require.NotNil(t, ev.Company)
	assert.Equal(t, "ACME-TH", *ev.Company)
	assert.GreaterOrEqual(t, ev.Seq, int64(1))

	// Informational only: the 250 ms target belongs to sub-project C.
	if emitted, err := time.Parse(time.RFC3339Nano, ev.Time); err == nil {
		t.Logf("emit→consume %s (envelope time %s)", r.at.Sub(emitted).Round(time.Millisecond), ev.Time)
	}
	if pub, err := publishedAt(t.Context(), rdb, e.stream, ev.ID); err == nil {
		t.Logf("publish→consume %s (XADD at %s; Redis and host clocks may differ slightly)",
			r.at.Sub(pub).Round(time.Millisecond), pub.Format(time.RFC3339Nano))
	} else {
		t.Logf("publish→consume not measured: %v", err)
	}
}
