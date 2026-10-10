package world

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp"
	pb "github.com/atipongsena/erp-town/go/internal/worldpb/world/v1"
)

const (
	roomTick          = 100 * time.Millisecond
	clientQueueSize   = 256
	resumeTTL         = 30 * time.Second
	maxResumeSessions = 1024
)

var (
	errResync   = errors.New("resync required")
	errShutdown = errors.New("world shutting down")
	errExpired  = errors.New("authentication expired")
)

type client struct {
	ctx            context.Context
	cancel         context.CancelCauseFunc
	claims         auth.Claims
	company, token string
	queue          chan *pb.ServerFrame
	// These fields are owned only by the room actor, never the writer.
	seen    map[EntityKey]uint64
	version uint64
}

func newClient(ctx context.Context, claims auth.Claims, company, token string) *client {
	ctx, cancel := context.WithCancelCause(ctx)
	claims.Roles = slices.Clone(claims.Roles)
	claims.Companies = slices.Clone(claims.Companies)
	return &client{ctx: ctx, cancel: cancel, claims: claims, company: company, token: token, queue: make(chan *pb.ServerFrame, clientQueueSize), seen: map[EntityKey]uint64{}}
}

func (c *client) enqueue(frame *pb.ServerFrame) bool {
	if c.ctx.Err() != nil {
		return false
	}
	select {
	case c.queue <- frame:
		return true
	default:
		c.cancel(errResync)
		return false
	}
}

type (
	updateRequest struct {
		state State
		done  chan error
	}
	joinResult struct {
		client *client
		err    error
	}
	joinRequest struct {
		ctx     context.Context
		claims  auth.Claims
		token   string
		version uint64
		done    chan joinResult
	}
)

type (
	leaveRequest struct {
		client *client
		done   chan struct{}
	}
	resumeSession struct {
		scope   string
		version uint64
		seen    map[EntityKey]uint64
		expires time.Time
	}
)

type room struct {
	ctx      context.Context
	state    State
	updates  chan updateRequest
	joins    chan joinRequest
	leaves   chan leaveRequest
	clients  map[*client]bool
	sessions map[string]resumeSession
}

// Service owns one actor per company. Every actor owns its State and client views.
// Publish accepts immutable state; it has no ERP write capability.
type Service struct {
	ctx         context.Context
	cancel      context.CancelCauseFunc
	mu          sync.Mutex
	rooms       map[string]*room
	wg          sync.WaitGroup
	connections sync.WaitGroup
}

func NewService(ctx context.Context) *Service {
	ctx, cancel := context.WithCancelCause(ctx)
	return &Service{ctx: ctx, cancel: cancel, rooms: map[string]*room{}}
}

// Close cancels every actor. WebSocket writers inherit this cancellation.
func (s *Service) Close() {
	s.mu.Lock()
	s.cancel(errShutdown)
	s.mu.Unlock()
	s.wg.Wait()
	s.connections.Wait()
}

func (s *Service) beginConnection() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	s.connections.Add(1)
	return true
}

func (s *Service) getRoom(company string) (*room, error) {
	if strings.TrimSpace(company) == "" {
		return nil, erp.NewValidation("company", "company is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return nil, context.Cause(s.ctx)
	}
	if existing := s.rooms[company]; existing != nil {
		return existing, nil
	}
	state, err := NewState(company)
	if err != nil {
		return nil, err
	}
	r := &room{ctx: s.ctx, state: state, updates: make(chan updateRequest), joins: make(chan joinRequest), leaves: make(chan leaveRequest), clients: map[*client]bool{}, sessions: map[string]resumeSession{}}
	s.rooms[company] = r
	s.wg.Go(r.run)
	return r, nil
}

func (s *Service) Publish(ctx context.Context, state State) error {
	r, err := s.getRoom(state.Company())
	if err != nil {
		return err
	}
	request := updateRequest{state: state, done: make(chan error, 1)}
	select {
	case r.updates <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return context.Cause(s.ctx)
	}
	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return context.Cause(s.ctx)
	}
}

func (s *Service) join(ctx context.Context, claims auth.Claims, company, token string, version uint64) (*client, error) {
	if strings.TrimSpace(claims.Subject) == "" || !time.Now().Before(claims.ExpiresAt) {
		return nil, erp.ErrUnauthenticated
	}
	if !slices.Contains(claims.Companies, company) {
		return nil, erp.ErrForbidden
	}
	r, err := s.getRoom(company)
	if err != nil {
		return nil, err
	}
	request := joinRequest{ctx: ctx, claims: claims, token: token, version: version, done: make(chan joinResult, 1)}
	select {
	case r.joins <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, context.Cause(s.ctx)
	}
	// After acceptance, await the actor's response even if JOIN is cancelled, so
	// an allocated client can be released instead of leaking an invisible member.
	select {
	case result := <-request.done:
		if ctx.Err() != nil && result.client != nil {
			s.leave(result.client)
			return nil, ctx.Err()
		}
		return result.client, result.err
	case <-s.ctx.Done():
		return nil, context.Cause(s.ctx)
	}
}

func (s *Service) leave(c *client) {
	c.cancel(context.Canceled)
	s.mu.Lock()
	r := s.rooms[c.company]
	s.mu.Unlock()
	if r == nil {
		return
	}
	request := leaveRequest{client: c, done: make(chan struct{})}
	select {
	case r.leaves <- request:
	case <-s.ctx.Done():
		return
	}
	select {
	case <-request.done:
	case <-s.ctx.Done():
	}
}

func (r *room) run() {
	tick := time.NewTicker(roomTick)
	defer tick.Stop()
	defer func() {
		for c := range r.clients {
			c.cancel(errShutdown)
		}
	}()
	for {
		select {
		case <-r.ctx.Done():
			return
		case request := <-r.updates:
			if request.state.Version() < r.state.Version() {
				request.done <- errors.New("world: room version regression")
				continue
			}
			r.state = request.state
			request.done <- nil
		case request := <-r.joins:
			r.join(request)
		case request := <-r.leaves:
			c := request.client
			if r.clients[c] {
				delete(r.clients, c)
				// Overflow means the client cannot prove receipt of the queued prefix.
				if !errors.Is(context.Cause(c.ctx), errResync) {
					r.sessions[c.token] = resumeSession{scope: visibilityScope(c.claims, c.company), version: c.version, seen: maps.Clone(c.seen), expires: time.Now().Add(resumeTTL)}
				}
			}
			r.purgeSessions(time.Now())
			close(request.done)
		case now := <-tick.C:
			r.purgeSessions(now)
			for c := range r.clients {
				if c.ctx.Err() != nil {
					continue
				}
				if !now.Before(c.claims.ExpiresAt) {
					c.cancel(errExpired)
					continue
				}
				if c.version == r.state.Version() {
					continue
				}
				frame, seen := frameFor(r.state, c.claims, c.seen, c.version, c.token, false)
				if len(frame.Upserts) == 0 && len(frame.Removes) == 0 {
					continue
				}
				if c.enqueue(frame) {
					c.seen, c.version = seen, frame.Version
				}
			}
		}
	}
}

func (r *room) join(request joinRequest) {
	if request.ctx.Err() != nil {
		request.done <- joinResult{err: request.ctx.Err()}
		return
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		request.done <- joinResult{err: fmt.Errorf("world: reconnect token: %w", err)}
		return
	}
	c := newClient(r.ctx, request.claims, r.state.Company(), base64.RawURLEncoding.EncodeToString(random[:]))
	snapshot := true
	if session, exists := r.sessions[request.token]; exists {
		delete(r.sessions, request.token) // consume the token; every connection rotates it
		if time.Now().Before(session.expires) && session.scope == visibilityScope(request.claims, r.state.Company()) && session.version == request.version && session.version <= r.state.Version() {
			c.seen, c.version, snapshot = maps.Clone(session.seen), session.version, false
		}
	}
	frame, seen := frameFor(r.state, request.claims, c.seen, c.version, c.token, snapshot)
	c.enqueue(frame)
	c.seen, c.version = seen, frame.Version
	r.clients[c] = true
	request.done <- joinResult{client: c}
}

func visibilityScope(claims auth.Claims, company string) string {
	roles := slices.Clone(claims.Roles)
	slices.Sort(roles)
	roles = slices.Compact(roles)
	return fmt.Sprintf("%q/%q/%q", claims.Subject, company, roles)
}

func (r *room) purgeSessions(now time.Time) {
	for token, session := range r.sessions {
		if !now.Before(session.expires) {
			delete(r.sessions, token)
		}
	}
	for len(r.sessions) > maxResumeSessions {
		var oldestToken string
		var oldest time.Time
		for token, session := range r.sessions {
			if oldestToken == "" || session.expires.Before(oldest) {
				oldestToken, oldest = token, session.expires
			}
		}
		delete(r.sessions, oldestToken)
	}
}
