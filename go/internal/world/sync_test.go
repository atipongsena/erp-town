package world

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events"
	pb "github.com/atipongsena/erp-town/go/internal/worldpb/world/v1"
)

func viewer() auth.Claims {
	return auth.Claims{Subject: "sales@acme-th.test", Roles: []string{"Sales User"}, Companies: []string{"ACME-TH"}, ExpiresAt: time.Now().Add(time.Minute)}
}

func TestVisibleEntitiesFailClosed(t *testing.T) {
	p, state := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	for i, visibility := range []events.Visibility{{Level: "public"}, {Level: "roles", Roles: []string{"Sales User"}}, {Level: "roles", Roles: []string{"Accounts User"}}, {Level: "private"}, {Level: "private"}} {
		e.Subject.Name = []string{"public", "allowed-role", "other-role", "owned", "other-user"}[i]
		e.Actor = events.Actor{Kind: "user", ID: "user." + viewer().Subject}
		if i == 4 {
			e.Actor.ID = "user.other@acme-th.test"
		}
		e.Visibility = visibility
		state = apply(t, p, state, e)
	}
	visible := VisibleEntities(state, viewer())
	require.Len(t, visible, 3)
	for _, entity := range visible {
		assert.NotContains(t, []string{"other-role", "other-user"}, entity.Key.Subject.Name)
	}
	other := viewer()
	other.Companies = []string{"OTHER"}
	assert.Empty(t, VisibleEntities(state, other))
	private := Entity{Actor: events.Actor{Kind: "agent", ID: "agent.1", OnBehalfOf: new("user." + viewer().Subject)}, Visibility: events.Visibility{Level: "private"}}
	assert.False(t, visibleTo(private, viewer()), "on_behalf_of never grants private ownership")
	assert.False(t, visibleTo(Entity{Visibility: events.Visibility{Level: "unknown"}}, viewer()))
}

func TestRoomPatchesRemoveVisibilityLossAndResume(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service := NewService(ctx)
	defer service.Close()
	p, state := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	state = apply(t, p, state, e)
	require.NoError(t, service.Publish(ctx, state))
	client, err := service.join(ctx, viewer(), state.Company(), "", 0)
	require.NoError(t, err)
	first := <-client.queue
	assert.Equal(t, pb.FrameKind_FRAME_KIND_SNAPSHOT, first.Kind)
	require.Len(t, first.Upserts, 1)
	e.Seq++
	e.Visibility = events.Visibility{Level: "roles", Roles: []string{"Accounts User"}}
	state = apply(t, p, state, e)
	require.NoError(t, service.Publish(ctx, state))
	select {
	case patch := <-client.queue:
		assert.Equal(t, pb.FrameKind_FRAME_KIND_PATCH, patch.Kind)
		assert.Equal(t, first.Version, patch.BaseVersion)
		assert.Empty(t, patch.Upserts)
		require.Len(t, patch.Removes, 1)
		service.leave(client)
		resumed, err := service.join(ctx, viewer(), state.Company(), patch.ResumeToken, patch.Version)
		require.NoError(t, err)
		assert.Equal(t, pb.FrameKind_FRAME_KIND_PATCH, (<-resumed.queue).Kind)
		service.leave(resumed)
		changed := viewer()
		changed.Roles = []string{"Accounts User"}
		fresh, err := service.join(ctx, changed, state.Company(), patch.ResumeToken, patch.Version)
		require.NoError(t, err)
		snapshot := <-fresh.queue
		assert.Equal(t, pb.FrameKind_FRAME_KIND_SNAPSHOT, snapshot.Kind)
		require.Len(t, snapshot.Upserts, 1)
	case <-time.After(2 * time.Second):
		t.Fatal("no visibility removal patch")
	}
}

func TestSlowClientQueueRequiresResync(t *testing.T) {
	client := newClient(t.Context(), viewer(), "ACME-TH", "token")
	for i := 0; i < 256; i++ {
		require.True(t, client.enqueue(&pb.ServerFrame{}))
	}
	require.False(t, client.enqueue(&pb.ServerFrame{}))
	require.ErrorContains(t, context.Cause(client.ctx), "resync")
}

func TestRoomResumeExpiryAndNoUnchangedPatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewService(t.Context())
		defer service.Close()
		p, state := setup(t)
		e := fixture(t, "erp.sales_order.submitted")
		state = apply(t, p, state, e)
		require.NoError(t, service.Publish(t.Context(), state))
		c, err := service.join(t.Context(), viewer(), state.Company(), "", 0)
		require.NoError(t, err)
		first := <-c.queue
		time.Sleep(3 * roomTick)
		synctest.Wait()
		assert.Empty(t, c.queue, "unchanged state sends no patch")
		service.leave(c)
		time.Sleep(31 * time.Second)
		synctest.Wait()
		fresh, err := service.join(t.Context(), viewer(), state.Company(), first.ResumeToken, first.Version)
		require.NoError(t, err)
		assert.Equal(t, pb.FrameKind_FRAME_KIND_SNAPSHOT, (<-fresh.queue).Kind)
	})
}

func TestRoomReconnectCannotReuseAnotherView(t *testing.T) {
	for _, change := range []string{"user", "roles", "version"} {
		t.Run(change, func(t *testing.T) {
			service := NewService(t.Context())
			defer service.Close()
			p, state := setup(t)
			e := fixture(t, "erp.sales_order.submitted")
			e.Visibility = events.Visibility{Level: "private"}
			e.Actor = events.Actor{Kind: "user", ID: "user." + viewer().Subject}
			state = apply(t, p, state, e)
			require.NoError(t, service.Publish(t.Context(), state))
			c, err := service.join(t.Context(), viewer(), state.Company(), "", 0)
			require.NoError(t, err)
			first := <-c.queue
			service.leave(c)
			claims := viewer()
			version := first.Version
			switch change {
			case "user":
				claims.Subject = "other@example.test"
			case "roles":
				claims.Roles = []string{"Accounts User"}
			case "version":
				version++
			}
			fresh, err := service.join(t.Context(), claims, state.Company(), first.ResumeToken, version)
			require.NoError(t, err)
			snapshot := <-fresh.queue
			assert.Equal(t, pb.FrameKind_FRAME_KIND_SNAPSHOT, snapshot.Kind)
			if change == "user" {
				assert.Empty(t, snapshot.Upserts)
			} else {
				require.Len(t, snapshot.Upserts, 1)
			}
		})
	}
}

func TestWebSocketRejectsCrossOriginAndInvalidJWT(t *testing.T) {
	_, _, url := wsFixture(t)
	conn, response, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}}})
	require.Error(t, err)
	require.Nil(t, conn)
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	conn, _, err = websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	raw, err := proto.Marshal(&pb.ClientFrame{Message: &pb.ClientFrame_Join{Join: &pb.Join{Jwt: "invalid", Company: "ACME-TH"}}})
	require.NoError(t, err)
	require.NoError(t, conn.Write(t.Context(), websocket.MessageBinary, raw))
	_, _, err = conn.Read(t.Context())
	require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
}

type verifiedViewer struct{ claims auth.Claims }

func (v verifiedViewer) Verify(string) (auth.Claims, error) { return v.claims, nil }

func TestWebSocketClosesWithExpiryAndShutdownReason(t *testing.T) {
	for _, cause := range []string{"expiry", "shutdown", "parent-shutdown", "intent"} {
		t.Run(cause, func(t *testing.T) {
			parent, stop := context.WithCancel(t.Context())
			defer stop()
			service := NewService(parent)
			defer service.Close()
			claims := viewer()
			if cause == "expiry" {
				claims.ExpiresAt = time.Now().Add(200 * time.Millisecond)
			}
			server := httptest.NewServer(NewHandler(service, verifiedViewer{claims}))
			defer server.Close()
			conn, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer func() { _ = conn.CloseNow() }()
			raw, err := proto.Marshal(&pb.ClientFrame{Message: &pb.ClientFrame_Join{Join: &pb.Join{Jwt: "verified-by-test", Company: "ACME-TH"}}})
			require.NoError(t, err)
			require.NoError(t, conn.Write(t.Context(), websocket.MessageBinary, raw))
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			_, _, err = conn.Read(ctx)
			require.NoError(t, err)
			switch cause {
			case "shutdown":
				service.cancel(errShutdown)
			case "parent-shutdown":
				stop()
			case "intent":
				raw, err = proto.Marshal(&pb.ClientFrame{Message: &pb.ClientFrame_DisplayIntent{DisplayIntent: &pb.DisplayIntent{Name: "erp.submit"}}})
				require.NoError(t, err)
				require.NoError(t, conn.Write(ctx, websocket.MessageBinary, raw))
			}
			_, _, err = conn.Read(ctx)
			if cause == "shutdown" || cause == "parent-shutdown" {
				require.Equal(t, websocket.StatusGoingAway, websocket.CloseStatus(err))
				require.ErrorContains(t, err, "world shutting down")
			} else {
				require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
				if cause == "expiry" {
					require.ErrorContains(t, err, "authentication expired")
				}
			}
		})
	}
}

func TestWebSocketJoinDeadlineSendsNoRoomData(t *testing.T) {
	service, _, url := wsFixture(t)
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	started := time.Now()
	_, raw, err := conn.Read(ctx)
	require.Error(t, err)
	assert.Empty(t, raw)
	assert.GreaterOrEqual(t, time.Since(started), 5*time.Second)
	service.mu.Lock()
	assert.Empty(t, service.rooms, "unauthenticated connection must not allocate a room")
	service.mu.Unlock()
}

func wsFixture(t *testing.T) (*Service, *auth.JWTIssuer, string) {
	t.Helper()
	service := NewService(t.Context())
	t.Cleanup(service.Close)
	issuer, err := auth.NewJWTIssuer(base64.StdEncoding.EncodeToString(make([]byte, 32)), time.Minute)
	require.NoError(t, err)
	server := httptest.NewServer(NewHandler(service, issuer))
	t.Cleanup(server.Close)
	return service, issuer, "ws" + strings.TrimPrefix(server.URL, "http")
}

func wsJoin(t *testing.T, url string, issuer *auth.JWTIssuer, company string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	claims := viewer()
	token, _, err := issuer.Issue(erp.Principal{ID: claims.Subject, Roles: claims.Roles, Companies: claims.Companies})
	require.NoError(t, err)
	raw, err := proto.Marshal(&pb.ClientFrame{Message: &pb.ClientFrame_Join{Join: &pb.Join{Jwt: token, Company: company}}})
	require.NoError(t, err)
	require.NoError(t, conn.Write(t.Context(), websocket.MessageBinary, raw))
	return conn
}

func TestWebSocketRequiresJWTCompanyAndBinaryJoin(t *testing.T) {
	_, issuer, url := wsFixture(t)
	for _, company := range []string{"OTHER", ""} {
		conn := wsJoin(t, url, issuer, company)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		_, _, err := conn.Read(ctx)
		cancel()
		require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
	}
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	require.NoError(t, conn.Write(t.Context(), websocket.MessageText, []byte("hello")))
	_, _, err = conn.Read(t.Context())
	require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
}

func TestWebSocketSendsFilteredBinarySnapshotAndPatch(t *testing.T) {
	service, issuer, url := wsFixture(t)
	p, state := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	state = apply(t, p, state, e)
	require.NoError(t, service.Publish(t.Context(), state))
	conn := wsJoin(t, url, issuer, "ACME-TH")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	kind, raw, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageBinary, kind)
	var first pb.ServerFrame
	require.NoError(t, proto.Unmarshal(raw, &first))
	require.Len(t, first.Upserts, 1)
	e.Seq++
	e.Data = []byte(`{"status":"Updated","party":"CUST-001","item_count":1,"delivery_date":"2026-10-08"}`)
	state = apply(t, p, state, e)
	require.NoError(t, service.Publish(ctx, state))
	_, raw, err = conn.Read(ctx)
	require.NoError(t, err)
	var patch pb.ServerFrame
	require.NoError(t, proto.Unmarshal(raw, &patch))
	require.Equal(t, pb.FrameKind_FRAME_KIND_PATCH, patch.Kind)
	require.Equal(t, first.Version, patch.BaseVersion)
	require.Len(t, patch.Upserts, 1)
	assert.Contains(t, string(patch.Upserts[0].DataJson), "Updated")
}
