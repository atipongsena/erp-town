// Command worldload observes a running world service without publishing events
// or writing ERP data. The isolated acceptance test measures stream latency.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/atipongsena/erp-town/go/internal/worldpb/world/v1"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	address := flag.String("url", "ws://127.0.0.1:8090/town/ws", "WebSocket endpoint; no query or credentials")
	company := flag.String("company", "", "authorized company abbreviation")
	clients := flag.Int("clients", 200, "number of connections (1-1000)")
	duration := flag.Duration("duration", 30*time.Second, "observation duration after all JOINs (up to 5m)")
	flag.Parse()
	u, err := url.Parse(*address)
	if err != nil || u == nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("url must be ws/wss with no query, fragment or credentials")
	}
	if *company == "" || *clients < 1 || *clients > 1000 || *duration <= 0 || *duration > 5*time.Minute {
		return errors.New("company, 1-1000 clients and duration in (0,5m] are required")
	}
	jwt := os.Getenv("TOWN_LOAD_JWT")
	if jwt == "" {
		return errors.New("TOWN_LOAD_JWT must contain a short-lived town JWT; never pass it in the URL")
	}
	join, err := proto.Marshal(&pb.ClientFrame{Message: &pb.ClientFrame_Join{Join: &pb.Join{Jwt: jwt, Company: *company}}})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connections := []*websocket.Conn{}
	var readers sync.WaitGroup
	defer func() {
		cancel()
		for _, conn := range connections {
			_ = conn.CloseNow()
		}
		readers.Wait()
	}()
	var frames, wireBytes atomic.Uint64
	errorsSeen := make(chan error, 1)
	for range *clients {
		joinCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		conn, _, err := websocket.Dial(joinCtx, *address, nil)
		if err != nil {
			stop()
			return errors.New("world connection failed") // no URL, token or response data in logs
		}
		connections = append(connections, conn)
		if err := conn.Write(joinCtx, websocket.MessageBinary, join); err != nil {
			stop()
			return errors.New("world JOIN write failed")
		}
		kind, raw, err := conn.Read(joinCtx)
		stop()
		frame := new(pb.ServerFrame)
		if err != nil || kind != websocket.MessageBinary || proto.Unmarshal(raw, frame) != nil || frame.Kind != pb.FrameKind_FRAME_KIND_SNAPSHOT || frame.Company != *company {
			return errors.New("world JOIN did not return an authorized snapshot")
		}
		readers.Go(func() {
			for {
				kind, raw, err := conn.Read(ctx)
				frame := new(pb.ServerFrame)
				if err != nil || kind != websocket.MessageBinary || proto.Unmarshal(raw, frame) != nil || frame.Company != *company {
					if ctx.Err() == nil {
						select {
						case errorsSeen <- errors.New("world observer disconnected or received an invalid frame"):
						default:
						}
					}
					return
				}
				frames.Add(1)
				wireBytes.Add(uint64(len(raw)))
			}
		})
	}
	timer := time.NewTimer(*duration)
	defer timer.Stop()
	select {
	case err := <-errorsSeen:
		return err
	case <-timer.C:
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Clients   int    `json:"clients"`
		Duration  string `json:"duration"`
		Frames    uint64 `json:"frames"`
		WireBytes uint64 `json:"wire_bytes"`
	}{*clients, duration.String(), frames.Load(), wireBytes.Load()})
}
