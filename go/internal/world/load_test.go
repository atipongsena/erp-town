package world

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events/eventstest"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	pb "github.com/atipongsena/erp-town/go/internal/worldpb/world/v1"
)

func TestWorld200ClientsStreamToPatchP95(t *testing.T) {
	const clients, rounds = 200, 20
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	service := NewService(ctx)
	defer service.Close()
	m, _ := platform.NewTestMetrics()
	reader := &projectionReader{snapshot: erp.ProjectionSnapshot{Company: "ACME-TH", Facts: []erp.ProjectionFact{}, Sequences: []erp.ProjectionSequence{}}}
	runtime, err := NewRuntime(service, rdb, pool, m, reader, erp.Principal{}, DefaultRuntimeConfig(stream))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	defer func() { cancel(); require.NoError(t, <-done) }()
	require.NoError(t, runtime.BeforeJoin(ctx, "ACME-TH"))
	issuer, err := auth.NewJWTIssuer(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)), time.Minute)
	require.NoError(t, err)
	jwt, _, err := issuer.Issue(erp.Principal{ID: "load@example.test", Companies: []string{"ACME-TH"}})
	require.NoError(t, err)
	server := httptest.NewServer(NewHandler(service, issuer))
	defer server.Close()
	join, err := proto.Marshal(&pb.ClientFrame{Message: &pb.ClientFrame_Join{Join: &pb.Join{Jwt: jwt, Company: "ACME-TH"}}})
	require.NoError(t, err)
	connections := []*websocket.Conn{}
	var readers sync.WaitGroup
	defer func() {
		for _, conn := range connections {
			_ = conn.CloseNow()
		}
		readers.Wait()
	}()
	type arrival struct {
		client int
		name   string
		at     time.Time
		err    error
	}
	arrivals := make(chan arrival, clients*rounds+clients)
	for client := range clients {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
		require.NoError(t, err)
		connections = append(connections, conn)
		require.NoError(t, conn.Write(ctx, websocket.MessageBinary, join))
		kind, raw, err := conn.Read(ctx)
		require.NoError(t, err)
		require.Equal(t, websocket.MessageBinary, kind)
		frame := new(pb.ServerFrame)
		require.NoError(t, proto.Unmarshal(raw, frame))
		require.Equal(t, pb.FrameKind_FRAME_KIND_SNAPSHOT, frame.Kind)
		readers.Go(func() {
			for {
				kind, raw, err := conn.Read(ctx)
				at := time.Now()
				if err != nil {
					arrivals <- arrival{client: client, err: err}
					return
				}
				frame := new(pb.ServerFrame)
				if kind != websocket.MessageBinary {
					arrivals <- arrival{client: client, err: fmt.Errorf("nonbinary patch")}
					return
				}
				if err := proto.Unmarshal(raw, frame); err != nil {
					arrivals <- arrival{client: client, err: err}
					return
				}
				for _, entity := range frame.Upserts {
					arrivals <- arrival{client: client, name: entity.Key.Name, at: at}
				}
			}
		})
	}
	latencies := []time.Duration{}
	for round := range rounds {
		e := fixture(t, "erp.sales_order.submitted")
		e.ID = ulid.Make().String()
		e.Subject.Name = fmt.Sprintf("SO-LOAD-%02d", round)
		e.Seq = 1
		e.Visibility.Level = "public"
		e.Visibility.Roles = nil
		entry := addWorldEvent(t, rdb, stream, e)
		milliseconds, err := strconv.ParseInt(strings.Split(entry, "-")[0], 10, 64)
		require.NoError(t, err)
		started := time.UnixMilli(milliseconds) // Redis entry time, not event business time
		seen := map[int]bool{}
		for range clients {
			select {
			case received := <-arrivals:
				require.NoError(t, received.err)
				require.Equal(t, e.Subject.Name, received.name)
				require.False(t, seen[received.client], "each client must receive this event exactly once")
				seen[received.client] = true
				latencies = append(latencies, received.at.Sub(started))
			case <-ctx.Done():
				t.Fatal("not all 200 clients received a patch before deadline")
			}
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95 := latencies[(len(latencies)*95+99)/100-1]
	t.Logf("clients=%d rounds=%d samples=%d stream-to-patch p95=%s max=%s", clients, rounds, len(latencies), p95, latencies[len(latencies)-1])
	require.Less(t, p95, 250*time.Millisecond)
}
