package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events/eventstest"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	pb "github.com/atipongsena/erp-town/go/internal/worldpb/world/v1"
)

func TestWireWorldServesAuthenticatedProjectionAndShutsDown(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	dbURL, err := url.Parse(pool.Config().ConnString())
	require.NoError(t, err)
	query := dbURL.Query()
	query.Set("search_path", pool.Config().ConnConfig.RuntimeParams["search_path"])
	dbURL.RawQuery = query.Encode()
	erpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "token fixture-reader:fixture-secret", r.Header.Get("Authorization"))
		if r.URL.Path == "/api/method/erp_town.session_api.whoami" {
			_, _ = w.Write([]byte(`{"message":{"user":"reader@example.test","roles":["Town Projection Reader"],"companies":["ACME-TH"]}}`))
			return
		}
		require.Equal(t, "/api/method/erp_town.projection_api.read_projection", r.URL.Path)
		_, _ = w.Write([]byte(`{"message":{"company":"ACME-TH","facts":[{"doctype":"Sales Order","name":"SO-1","kind":"document","status":"To Deliver"}],"sequences":[{"doctype":"Sales Order","name":"SO-1","seq":1}]}}`))
	}))
	defer erpServer.Close()
	seed := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	cfg := platform.Config{DatabaseURL: dbURL.String(), RedisURL: "redis://" + rdb.Options().Addr, Stream: stream, ERPURL: erpServer.URL, JWTPrivateKey: seed, WorldAPIKey: "fixture-reader", WorldAPISecret: "fixture-secret"}
	mux := platform.NewMux()
	m, _ := platform.NewTestMetrics()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	run, closeWorld, err := wireWorld(ctx, mux, cfg, m)
	require.NoError(t, err)
	defer closeWorld()
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	defer func() { cancel(); require.NoError(t, <-done) }()
	server := httptest.NewServer(mux)
	defer server.Close()
	issuer, err := auth.NewJWTIssuer(seed, time.Minute)
	require.NoError(t, err)
	jwt, _, err := issuer.Issue(erp.Principal{ID: "sales@example.test", Companies: []string{"ACME-TH"}})
	require.NoError(t, err)
	conn, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http")+"/town/ws", nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	join, err := proto.Marshal(&pb.ClientFrame{Message: &pb.ClientFrame_Join{Join: &pb.Join{Jwt: jwt, Company: "ACME-TH"}}})
	require.NoError(t, err)
	require.NoError(t, conn.Write(t.Context(), websocket.MessageBinary, join))
	readCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	kind, raw, err := conn.Read(readCtx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageBinary, kind)
	frame := new(pb.ServerFrame)
	require.NoError(t, proto.Unmarshal(raw, frame))
	require.Equal(t, pb.FrameKind_FRAME_KIND_SNAPSHOT, frame.Kind)
	require.Len(t, frame.Upserts, 1)
	require.Equal(t, "SO-1", frame.Upserts[0].Key.Name)
}
