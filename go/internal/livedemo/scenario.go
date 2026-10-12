// Package livedemo coordinates a bounded local demonstration over the real ERP
// port. Its worker identities are explicit test fixtures, not human presence.
package livedemo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/atipongsena/erp-town/go/internal/agents"
	"github.com/atipongsena/erp-town/go/internal/erp"
)

type Reference struct {
	DocType string `json:"doctype"`
	Name    string `json:"name"`
}
type Document struct {
	Reference
	Status string `json:"status"`
}
type Actor struct {
	ID             string       `json:"id"`
	Kind           string       `json:"kind"`
	Department     string       `json:"department"`
	FromDepartment string       `json:"from_department,omitempty"`
	Activity       string       `json:"activity"`
	Travel         *ActorTravel `json:"travel,omitempty"`
	TaskRef        *Reference   `json:"task_ref,omitempty"`
}
type ActorTravel struct {
	StartedAt  time.Time `json:"started_at"`
	DurationMS int64     `json:"duration_ms"`
}
type Status struct {
	LayoutVersion   string     `json:"layout_version"`
	Enabled         bool       `json:"enabled"`
	Mode            string     `json:"mode"`
	MotionMode      string     `json:"motion_mode"`
	Company         string     `json:"company"`
	Round           int        `json:"round"`
	MaxRounds       int        `json:"max_rounds"`
	Phase           string     `json:"phase"`
	Stage           string     `json:"stage"`
	StartedAt       time.Time  `json:"started_at"`
	StageStartedAt  time.Time  `json:"stage_started_at"`
	StageDurationMS int64      `json:"stage_duration_ms"`
	Actors          []Actor    `json:"actors"`
	Documents       []Document `json:"documents"`
	Error           string     `json:"error,omitempty"`
}
type (
	AgentDraft func(context.Context, int, erp.Date) (erp.DocRef, error)
	Scenario   struct {
		mu         sync.RWMutex
		state      Status
		port       erp.Port
		staff      map[string]erp.Principal
		agentDraft AgentDraft
		wait       func(context.Context, time.Duration) error
		now        func() time.Time
		review     time.Duration
	}
)

func New(port erp.Port, staff map[string]erp.Principal, draft AgentDraft, rounds int) (*Scenario, error) {
	if port == nil || draft == nil || rounds < 1 || rounds > 3 {
		return nil, errors.New("invalid bounded demo configuration")
	}
	for _, department := range []string{"shop", "warehouse", "bank"} {
		if staff[department].ID == "" || staff[department].Credential == nil {
			return nil, errors.New("missing test staff")
		}
	}
	now := time.Now().UTC()
	actors := []Actor{}
	for _, d := range []string{"shop", "warehouse", "bank"} {
		actors = append(actors, Actor{ID: "demo-" + d, Kind: "human", Department: "hq", Activity: "idle"})
	}
	actors = append(actors, Actor{ID: "demo-assistant", Kind: "agent", Department: "it", Activity: "idle"})
	return &Scenario{port: port, staff: staff, agentDraft: draft, wait: pause, now: time.Now, review: 12 * time.Second, state: Status{LayoutVersion: layout.Version, Enabled: true, Mode: "erp-test", MotionMode: "loop", Company: "ACME-TH", MaxRounds: rounds, Phase: "starting", Stage: "starting", StartedAt: now, StageStartedAt: now, Actors: actors, Documents: []Document{}}}, nil
}

func (s *Scenario) Snapshot() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.state
	out.Actors = slices.Clone(out.Actors)
	out.Documents = slices.Clone(out.Documents)
	for i := range out.Actors {
		if out.Actors[i].Travel != nil {
			travel := *out.Actors[i].Travel
			out.Actors[i].Travel = &travel
		}
		if out.Actors[i].TaskRef != nil {
			ref := *out.Actors[i].TaskRef
			out.Actors[i].TaskRef = &ref
		}
	}
	return out
}

func (s *Scenario) stage(stage, phase, actor, department string, duration time.Duration, ref *erp.DocRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Stage, s.state.Phase = stage, phase
	s.state.StageStartedAt, s.state.StageDurationMS = s.now().UTC(), duration.Milliseconds()
	for i := range s.state.Actors {
		a := &s.state.Actors[i]
		a.Activity = "idle"
		a.Travel = nil
		a.TaskRef = nil
		a.FromDepartment = ""
		if a.ID == actor {
			a.FromDepartment = a.Department
			a.Department = department
			a.Activity = phase
			if ref != nil {
				a.TaskRef = &Reference{DocType: string(ref.DocType), Name: ref.Name}
			} else {
				a.TaskRef = nil
			}
		}
	}
}

func (s *Scenario) record(ref erp.DocRef, status erp.DocStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.state.Documents {
		if d.DocType == string(ref.DocType) && d.Name == ref.Name {
			s.state.Documents[i].Status = string(status)
			return
		}
	}
	s.state.Documents = append(s.state.Documents, Document{Reference: Reference{DocType: string(ref.DocType), Name: ref.Name}, Status: string(status)})
}

func pause(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Scenario) travelTo(ctx context.Context, stage, actor, department string, ref *erp.DocRef) error {
	state := s.Snapshot()
	var moving Actor
	for _, a := range state.Actors {
		if a.ID == actor {
			moving = a
		}
	}
	route, err := RouteFor(moving.Department, department, moving.Kind)
	if err != nil {
		return err
	}
	duration := TravelDuration(route)
	s.stage(stage, "travelling", actor, department, duration, ref)
	s.mu.Lock()
	for i := range s.state.Actors {
		if s.state.Actors[i].ID == actor {
			s.state.Actors[i].Travel = &ActorTravel{StartedAt: s.state.StageStartedAt, DurationMS: duration.Milliseconds()}
		}
	}
	s.mu.Unlock()
	return s.wait(ctx, duration)
}

func (s *Scenario) reviewDocument(ctx context.Context, stage, actor, department string, ref erp.DocRef) error {
	s.stage(stage, "working", actor, department, s.review, &ref)
	as := s.staff[department]
	var err error
	switch ref.DocType {
	case erp.DocSalesOrder:
		_, err = s.port.GetSalesOrder(ctx, as, ref.Name)
	case erp.DocDeliveryNote:
		_, err = s.port.GetDeliveryNote(ctx, as, ref.Name)
	case erp.DocSalesInvoice:
		_, err = s.port.GetSalesInvoice(ctx, as, ref.Name)
	case erp.DocPaymentEntry:
		_, err = s.port.GetPaymentEntry(ctx, as, ref.Name)
	default:
		return errors.New("unknown demo document")
	}
	if err != nil {
		return err
	}
	return s.wait(ctx, s.review)
}

func (s *Scenario) submit(ctx context.Context, ref erp.DocRef, department string) error {
	out, err := s.port.Submit(ctx, s.staff[department], ref)
	if err != nil {
		return err
	}
	if !out.Submitted {
		return errors.New("demo requires human workflow action")
	}
	s.record(ref, erp.DocSubmitted)
	return nil
}

// Run has no write retries. An uncertain ERP response halts the demonstration;
// restarting requires the launcher to restore the snapshot and start fresh.
func (s *Scenario) Run(ctx context.Context) error {
	for round := 1; round <= s.Snapshot().MaxRounds; round++ {
		s.mu.Lock()
		s.state.Round = round
		s.mu.Unlock()
		if err := s.round(ctx, round); err != nil {
			s.stage("failed", "failed", "", "", 0, nil)
			s.mu.Lock()
			s.state.Error = "ERP_STEP_FAILED"
			s.mu.Unlock()
			return err
		}
	}
	s.stage("completed", "idle", "", "", 0, nil)
	return nil
}

func (s *Scenario) round(ctx context.Context, round int) error {
	date := erp.MustDate(s.now().In(time.FixedZone("ICT", 7*3600)).Format(time.DateOnly))
	if err := s.travelTo(ctx, "ai-order", "demo-assistant", "shop", nil); err != nil {
		return err
	}
	s.stage("ai-order", "working", "demo-assistant", "shop", s.review, nil)
	order, err := s.agentDraft(ctx, round, date)
	if err != nil {
		return err
	}
	s.record(order, erp.DocDraft)
	if err := s.travelTo(ctx, "sales-review", "demo-shop", "shop", &order); err != nil {
		return err
	}
	if err := s.reviewDocument(ctx, "sales-review", "demo-shop", "shop", order); err != nil {
		return err
	}
	if err := s.submit(ctx, order, "shop"); err != nil {
		return err
	}
	if err := s.travelTo(ctx, "stock-prepare", "demo-warehouse", "warehouse", &order); err != nil {
		return err
	}
	s.stage("stock-prepare", "working", "demo-warehouse", "warehouse", 0, &order)
	delivery, err := s.port.CreateDeliveryNote(ctx, s.staff["warehouse"], order, &date)
	if err != nil {
		return err
	}
	s.record(delivery, erp.DocDraft)
	if err := s.reviewDocument(ctx, "stock-prepare", "demo-warehouse", "warehouse", delivery); err != nil {
		return err
	}
	if err := s.submit(ctx, delivery, "warehouse"); err != nil {
		return err
	}
	// This dispatch is an explicit test worker task attached to the real DN.
	// ERP submission proves the document state, not a customer's GPS position.
	if err := s.travelTo(ctx, "dispatch", "demo-warehouse", "shop", &delivery); err != nil {
		return err
	}
	if err := s.travelTo(ctx, "invoice-review", "demo-bank", "bank", &delivery); err != nil {
		return err
	}
	s.stage("invoice-review", "working", "demo-bank", "bank", 0, &delivery)
	invoice, err := s.port.CreateSalesInvoice(ctx, s.staff["bank"], delivery, &date)
	if err != nil {
		return err
	}
	s.record(invoice, erp.DocDraft)
	if err := s.reviewDocument(ctx, "invoice-review", "demo-bank", "bank", invoice); err != nil {
		return err
	}
	if err := s.submit(ctx, invoice, "bank"); err != nil {
		return err
	}
	s.stage("payment-review", "working", "demo-bank", "bank", 0, &invoice)
	payment, err := s.port.CreatePayment(ctx, s.staff["bank"], erp.PaymentInput{Invoice: invoice.Name, ModeOfPayment: "เงินสด", PostingDate: &date})
	if err != nil {
		return err
	}
	s.record(payment, erp.DocDraft)
	if err := s.reviewDocument(ctx, "payment-review", "demo-bank", "bank", payment); err != nil {
		return err
	}
	if err := s.submit(ctx, payment, "bank"); err != nil {
		return err
	}
	s.stage("round-completed", "idle", "", "", s.review, nil)
	if err := s.wait(ctx, s.review); err != nil {
		return err
	}
	requests := []TravelRequest{}
	for _, a := range s.Snapshot().Actors {
		target := "hq"
		if a.Kind == "agent" {
			target = "it"
		}
		requests = append(requests, TravelRequest{ID: a.ID, From: a.Department, To: target, Kind: a.Kind})
	}
	start := s.now().UTC()
	trips, err := ScheduleTravel(requests, start)
	if err != nil {
		return err
	}
	deadline := start
	for _, trip := range trips {
		end := trip.StartedAt.Add(trip.Duration)
		if end.After(deadline) {
			deadline = end
		}
	}
	s.stage("return-to-hq", "travelling", "", "", deadline.Sub(start), nil)
	s.mu.Lock()
	s.state.StageStartedAt = start
	for i := range s.state.Actors {
		actor := &s.state.Actors[i]
		for _, request := range requests {
			if request.ID == actor.ID {
				actor.FromDepartment = request.From
				actor.Department = request.To
			}
		}
		for _, trip := range trips {
			if trip.ID == actor.ID && trip.Duration > 0 {
				actor.Activity = "travelling"
				actor.Travel = &ActorTravel{StartedAt: trip.StartedAt, DurationMS: trip.Duration.Milliseconds()}
			}
		}
		actor.TaskRef = nil
	}
	s.mu.Unlock()
	return s.wait(ctx, deadline.Sub(start))
}

// LocalProvider performs deterministic local planning. The bounded pause is
// local demo processing, not a claim that a remote model is executing.
type LocalProvider struct {
	Call agents.Call
	Pace time.Duration
}

func (p LocalProvider) Step(ctx context.Context, req agents.Request) (agents.Response, error) {
	if len(req.History) == 0 {
		if err := pause(ctx, p.Pace); err != nil {
			return agents.Response{}, err
		}
		return agents.Response{Calls: []agents.Call{p.Call}, Usage: agents.Usage{InputTokens: 1, OutputTokens: 1}}, nil
	}
	return agents.Response{Text: "The isolated test draft is ready for the seeded sales worker.", Usage: agents.Usage{InputTokens: 1, OutputTokens: 1}}, nil
}

func DraftWithAgent(port erp.Port, ledger *agents.PostgresLedger, template agents.Binding, delegator string) AgentDraft {
	return func(ctx context.Context, round int, date erp.Date) (erp.DocRef, error) {
		binding := template
		binding.RunID = fmt.Sprintf("LIVE-DEMO-%d", round)
		executor, err := agents.NewPolicyExecutor(port, ledger, binding)
		if err != nil {
			return erp.DocRef{}, err
		}
		if err = executor.Validate(ctx); err != nil {
			return erp.DocRef{}, err
		}
		if err = ledger.Create(ctx, binding.RunID, binding.AgentKey, binding.Company, delegator); err != nil {
			return erp.DocRef{}, err
		}
		claim, err := ledger.Start(ctx, binding.RunID)
		if err != nil {
			return erp.DocRef{}, err
		}
		if claim != agents.RunClaimNew {
			return erp.DocRef{}, agents.ErrAlreadyExecuted
		}
		arguments, _ := json.Marshal(map[string]any{"customer": "CUST-001", "transaction_date": date.String(), "delivery_date": date.String(), "lines": []map[string]string{{"item_code": "ITEM-001", "qty": "1"}}})
		call := agents.Call{ID: "order-draft", Tool: "create_sales_order_draft", Arguments: arguments}
		run, err := agents.NewRun(agents.Spec{ID: binding.RunID, Instruction: "Prepare one isolated demonstration order draft.", Tools: executor.Tools()}, LocalProvider{Call: call, Pace: 12 * time.Second}, executor, ledger, agents.DefaultLimits())
		if err != nil {
			return erp.DocRef{}, err
		}
		result, runErr := run.Execute(ctx)
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		err = ledger.CompleteRun(finishCtx, binding.RunID, result)
		finishCancel()
		if err != nil {
			return erp.DocRef{}, err
		}
		if runErr != nil {
			return erp.DocRef{}, runErr
		}
		if result.Status != agents.Completed {
			return erp.DocRef{}, errors.New("local agent stopped for human review")
		}
		// Replaying a recorded call reads the ledger; it never repeats the ERP write.
		output, err := executor.Execute(ctx, call)
		if err != nil {
			return erp.DocRef{}, err
		}
		var safe agents.SafeResult
		if json.Unmarshal(output.Output, &safe) != nil || safe.NeedsHuman || safe.Status != erp.DocDraft {
			return erp.DocRef{}, errors.New("invalid agent draft result")
		}
		return safe.Ref, nil
	}
}
