package world

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/atipongsena/erp-town/go/internal/auth"
	pb "github.com/atipongsena/erp-town/go/internal/worldpb/world/v1"
)

const joinTimeout = 5 * time.Second

type JWTVerifier interface {
	Verify(string) (auth.Claims, error)
}

// Handler authenticates before joining a room or sending any data. BeforeJoin is
// the runtime's recovery gate; nil is useful for an already initialized service.
type Handler struct {
	service    *Service
	verifier   JWTVerifier
	BeforeJoin func(context.Context, string) error
}

func NewHandler(service *Service, verifier JWTVerifier) *Handler {
	return &Handler{service: service, verifier: verifier}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.service.beginConnection() {
		http.Error(w, "world unavailable", http.StatusServiceUnavailable)
		return
	}
	defer h.service.connections.Done()
	conn, err := websocket.Accept(w, r, nil) // same-origin protection is the library default
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(16 * 1024)
	joinCtx, cancel := context.WithTimeout(h.service.ctx, joinTimeout)
	kind, raw, err := conn.Read(joinCtx)
	if err != nil {
		cancel()
		_ = conn.Close(websocket.StatusPolicyViolation, "authenticated JOIN required")
		return
	}
	var frame pb.ClientFrame
	if kind != websocket.MessageBinary || proto.Unmarshal(raw, &frame) != nil || frame.GetJoin() == nil {
		cancel()
		_ = conn.Close(websocket.StatusPolicyViolation, "binary JOIN required")
		return
	}
	join := frame.GetJoin()
	claims, err := h.verifier.Verify(join.Jwt)
	if err != nil {
		cancel()
		_ = conn.Close(websocket.StatusPolicyViolation, "authentication required")
		return
	}
	// Check company membership before invoking a recovery/ERP reader.
	if !containsCompany(claims, join.Company) {
		cancel()
		_ = conn.Close(websocket.StatusPolicyViolation, "company forbidden")
		return
	}
	if h.BeforeJoin != nil {
		if err := h.BeforeJoin(joinCtx, join.Company); err != nil {
			cancel()
			_ = conn.Close(websocket.StatusTryAgainLater, "world unavailable")
			return
		}
	}
	c, err := h.service.join(joinCtx, claims, join.Company, join.ResumeToken, join.LastVersion)
	cancel()
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "join denied")
		return
	}
	defer h.service.leave(c)
	done := make(chan struct{})
	go func() { defer close(done); h.write(conn, c) }()
	for {
		// The writer owns close handshakes. Cancelling a Read context force-closes
		// the socket and would erase the expiry/resync reason before it reaches
		// the client. Writer shutdown always unblocks this read.
		kind, raw, err := conn.Read(context.Background())
		if err != nil {
			c.cancel(context.Canceled)
			break
		}
		frame.Reset()
		if kind != websocket.MessageBinary || proto.Unmarshal(raw, &frame) != nil || !displayOnly(frame.GetDisplayIntent()) {
			c.cancel(errors.New("display intent required"))
			break
		}
	}
	<-done
}

func containsCompany(claims auth.Claims, company string) bool {
	if claims.Subject == "" || company == "" || !time.Now().Before(claims.ExpiresAt) {
		return false
	}
	for _, permitted := range claims.Companies {
		if company == permitted {
			return true
		}
	}
	return false
}

func displayOnly(intent *pb.DisplayIntent) bool {
	if intent == nil || len(intent.Target) > 128 {
		return false
	}
	return intent.Name == "camera.focus" || intent.Name == "avatar.emote"
}

func (h *Handler) write(conn *websocket.Conn, c *client) {
	expiry := time.NewTimer(time.Until(c.claims.ExpiresAt))
	defer expiry.Stop()
	defer func() {
		code, reason := websocket.StatusPolicyViolation, "display intent required"
		switch cause := context.Cause(c.ctx); {
		case errors.Is(cause, errResync):
			reason = "resync required"
		case errors.Is(cause, errExpired):
			reason = "authentication expired"
		case errors.Is(cause, errShutdown):
			code, reason = websocket.StatusGoingAway, "world shutting down"
		case errors.Is(cause, context.Canceled):
			code, reason = websocket.StatusNormalClosure, "connection closed"
			if h.service.ctx.Err() != nil {
				code, reason = websocket.StatusGoingAway, "world shutting down"
			}
		}
		_ = conn.Close(code, reason)
	}()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-expiry.C:
			c.cancel(errExpired)
			return
		case frame := <-c.queue:
			raw, err := proto.Marshal(frame)
			if err != nil {
				c.cancel(errResync)
				return
			}
			ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
			err = conn.Write(ctx, websocket.MessageBinary, raw)
			cancel()
			if err != nil {
				c.cancel(errResync)
				return
			}
		}
	}
}
