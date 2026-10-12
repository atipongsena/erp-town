package agents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Binding comes only from trusted server configuration and an authenticated
// delegation request. It must never be deserialized from provider arguments.
type Binding struct {
	RunID, AgentKey, Company             string
	AgentCredential, DelegatorCredential erp.Credential
}

type (
	Intent struct {
		RunID, CallID, Tool, Hash, AgentKey, Company, Delegator string
		Write                                                   bool
	}
	SafeResult struct {
		Ref        erp.DocRef    `json:"ref"`
		Status     erp.DocStatus `json:"status"`
		Code       string        `json:"code"`
		NeedsHuman bool          `json:"needs_human"`
	}
)
type ClaimState string

const (
	ClaimNew       ClaimState = "new"
	ClaimCompleted ClaimState = "completed"
	ClaimUncertain ClaimState = "uncertain"
	ClaimConflict  ClaimState = "conflict"
)

type (
	Claim struct {
		State  ClaimState
		Result SafeResult
	}
	PolicyLedger interface {
		Attempt(context.Context, string, string) (bool, error)
		Claim(context.Context, Intent) (Claim, error)
		Finish(context.Context, Intent, SafeResult) error
	}
)

var errPolicyStorage = errors.New("agents: policy storage unavailable")

type PolicyExecutor struct {
	port    erp.Port
	ledger  PolicyLedger
	binding Binding
}

func NewPolicyExecutor(p erp.Port, l PolicyLedger, b Binding) (*PolicyExecutor, error) {
	if p == nil || l == nil || !validID(b.RunID) || !validID(b.AgentKey) || strings.TrimSpace(b.Company) == "" || b.AgentCredential == nil || b.DelegatorCredential == nil {
		return nil, ErrInvalidRun
	}
	return &PolicyExecutor{port: p, ledger: l, binding: b}, nil
}

type capability struct {
	name   string
	dt     erp.DocType
	write  bool
	schema string
}

const (
	refSchema    = `{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":140}},"required":["name"],"additionalProperties":false}`
	sourceSchema = `{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":140},"posting_date":{"type":"string","format":"date"}},"required":["name"],"additionalProperties":false}`
)

var capabilities = []capability{
	{"read_sales_order", erp.DocSalesOrder, false, refSchema},
	{"read_delivery_note", erp.DocDeliveryNote, false, refSchema},
	{"read_sales_invoice", erp.DocSalesInvoice, false, refSchema},
	{"read_payment_entry", erp.DocPaymentEntry, false, refSchema},
	{"create_sales_order_draft", erp.DocSalesOrder, true, `{"type":"object","properties":{"customer":{"type":"string","minLength":1,"maxLength":140},"transaction_date":{"type":"string","format":"date"},"delivery_date":{"type":"string","format":"date"},"lines":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"object","properties":{"item_code":{"type":"string","minLength":1,"maxLength":140},"qty":{"type":"string","pattern":"^[0-9]+(\\.[0-9]+)?$"}},"required":["item_code","qty"],"additionalProperties":false}}},"required":["customer","transaction_date","delivery_date","lines"],"additionalProperties":false}`},
	{"create_delivery_note_draft", erp.DocDeliveryNote, true, sourceSchema},
	{"create_sales_invoice_draft", erp.DocSalesInvoice, true, sourceSchema},
	{"create_payment_draft", erp.DocPaymentEntry, true, `{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":140},"mode_of_payment":{"type":"string","minLength":1,"maxLength":140},"posting_date":{"type":"string","format":"date"}},"required":["name","mode_of_payment"],"additionalProperties":false}`},
}

func (x *PolicyExecutor) Tools() []ToolDefinition {
	tools := make([]ToolDefinition, 0, len(capabilities))
	for _, c := range capabilities {
		tools = append(tools, ToolDefinition{Name: c.name, Description: strings.ReplaceAll(c.name, "_", " ") + "; ERP authority and human confirmation apply.", InputSchema: json.RawMessage(c.schema)})
	}
	return tools
}

func (x *PolicyExecutor) authorities(ctx context.Context) (erp.Principal, erp.Principal, erp.AgentAccess, erp.AgentAccess, error) {
	a, err := x.port.AgentAccess(ctx, x.binding.AgentCredential)
	if err != nil {
		return erp.Principal{}, erp.Principal{}, a, erp.AgentAccess{}, erp.ErrForbidden
	}
	d, err := x.port.AgentAccess(ctx, x.binding.DelegatorCredential)
	if err != nil || a.AgentKey != x.binding.AgentKey || a.Identity.User == "" || d.Identity.User == "" || d.Identity.User == "Guest" || a.Identity.User == d.Identity.User || d.AgentKey != "" || !slices.Contains(a.Identity.Companies, x.binding.Company) || !slices.Contains(d.Identity.Companies, x.binding.Company) {
		return erp.Principal{}, erp.Principal{}, a, d, erp.ErrForbidden
	}
	agent := erp.Principal{Kind: erp.PrincipalAgent, ID: a.AgentKey, OnBehalfOf: d.Identity.User, Roles: a.Identity.Roles, Companies: a.Identity.Companies, Credential: a.Identity.Credential}
	delegate := erp.Principal{Kind: erp.PrincipalUser, ID: d.Identity.User, Roles: d.Identity.Roles, Companies: d.Identity.Companies, Credential: d.Identity.Credential}
	return agent, delegate, a, d, nil
}

func (x *PolicyExecutor) Validate(ctx context.Context) error {
	_, _, _, _, err := x.authorities(ctx)
	return err
}

func policyStop(code string) ToolResult {
	b, _ := json.Marshal(SafeResult{Code: code, NeedsHuman: true})
	return ToolResult{Output: b, NeedsHuman: true}
}

func (x *PolicyExecutor) Execute(ctx context.Context, call Call) (ToolResult, error) {
	if ctx.Err() != nil {
		return policyStop("CANCELLED"), nil
	}
	agent, delegate, a, d, err := x.authorities(ctx)
	if err != nil {
		return policyStop("AUTHORITY_REVOKED"), nil
	}
	admitted, err := x.ledger.Attempt(ctx, x.binding.AgentKey, x.binding.Company)
	if err != nil {
		return policyStop("POLICY_UNAVAILABLE"), nil
	}
	if !admitted {
		return policyStop("RATE_LIMIT"), nil
	}
	i := slices.IndexFunc(capabilities, func(c capability) bool { return c.name == call.Tool })
	if i < 0 || !validID(call.ID) {
		return policyStop("TOOL_DENIED"), nil
	}
	cap := capabilities[i]
	ap, dp := a.Permissions[cap.dt], d.Permissions[cap.dt]
	if (!cap.write && (!ap.Read || !dp.Read)) || (cap.write && (!ap.Create || !dp.Create)) {
		return policyStop("PERMISSION_DENIED"), nil
	}
	canonical, err := canonicalArguments(call.Arguments)
	if err != nil {
		return policyStop("INVALID_ARGUMENTS"), nil
	}
	perform, err := x.prepare(ctx, cap, canonical, agent, delegate)
	if err != nil {
		return policyStop("POLICY_DENIED"), nil
	}
	hash := sha256.Sum256(append([]byte(call.Tool+"\x00"), canonical...))
	intent := Intent{RunID: x.binding.RunID, CallID: call.ID, Tool: call.Tool, Hash: hex.EncodeToString(hash[:]), AgentKey: x.binding.AgentKey, Company: x.binding.Company, Delegator: delegate.ID, Write: cap.write}
	claim, err := x.ledger.Claim(ctx, intent)
	if err != nil {
		return policyStop("POLICY_UNAVAILABLE"), nil
	}
	switch claim.State {
	case ClaimCompleted:
		b, _ := json.Marshal(claim.Result)
		return ToolResult{Output: b, NeedsHuman: claim.Result.NeedsHuman}, nil
	case ClaimNew:
	default:
		return policyStop("INTENT_UNRESOLVED"), nil
	}
	// Recheck after source preparation and durable claiming, immediately before
	// the effect. Revocation never turns a recorded intent into permission.
	_, _, finalAgent, finalDelegate, err := x.authorities(ctx)
	fa, fd := finalAgent.Permissions[cap.dt], finalDelegate.Permissions[cap.dt]
	if err != nil || (!cap.write && (!fa.Read || !fd.Read)) || (cap.write && (!fa.Create || !fd.Create)) {
		result := SafeResult{Code: "AUTHORITY_REVOKED", NeedsHuman: true}
		if err := x.finish(ctx, intent, result); err != nil {
			return policyStop("AUDIT_UNAVAILABLE"), nil
		}
		return policyStop(result.Code), nil
	}
	result, err := invoke(func() (SafeResult, error) { return perform(ctx) })
	if err != nil {
		result = SafeResult{Code: "TOOL_UNCERTAIN", NeedsHuman: true}
	}
	if err := x.finish(ctx, intent, result); err != nil {
		return policyStop("AUDIT_UNAVAILABLE"), nil
	}
	b, _ := json.Marshal(result)
	return ToolResult{Output: b, NeedsHuman: result.NeedsHuman}, nil
}

func (x *PolicyExecutor) finish(ctx context.Context, in Intent, r SafeResult) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return x.ledger.Finish(cleanup, in, r)
}

func decodeArguments(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

func validName(s string) bool {
	return len(s) > 0 && len(s) <= 140 && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\r\n\x00")
}

func validUnits(lines []erp.Line) bool {
	if len(lines) == 0 || len(lines) > 100 {
		return false
	}
	total := erp.Decimal{}
	for _, l := range lines {
		if !validName(l.ItemCode) || l.Qty.Cmp(erp.Decimal{}) <= 0 {
			return false
		}
		total = total.Add(l.Qty)
	}
	return total.Cmp(erp.MustDecimal("100")) <= 0
}

func (x *PolicyExecutor) prepare(ctx context.Context, c capability, b []byte, as, delegate erp.Principal) (func(context.Context) (SafeResult, error), error) {
	if c.name == "create_sales_order_draft" {
		var args struct {
			Customer        string `json:"customer"`
			TransactionDate string `json:"transaction_date"`
			DeliveryDate    string `json:"delivery_date"`
			Lines           []struct {
				ItemCode string `json:"item_code"`
				Qty      string `json:"qty"`
			} `json:"lines"`
		}
		if err := decodeArguments(b, &args); err != nil || !validName(args.Customer) {
			return nil, erp.ErrValidation
		}
		td, err := erp.ParseDate(args.TransactionDate)
		if err != nil {
			return nil, erp.ErrValidation
		}
		dd, err := erp.ParseDate(args.DeliveryDate)
		if err != nil || dd.Before(td) {
			return nil, erp.ErrValidation
		}
		lines := []erp.LineInput{}
		check := []erp.Line{}
		for _, line := range args.Lines {
			qty, err := erp.ParseDecimal(line.Qty)
			if err != nil {
				return nil, erp.ErrValidation
			}
			lines = append(lines, erp.LineInput{ItemCode: line.ItemCode, Qty: qty})
			check = append(check, erp.Line{ItemCode: line.ItemCode, Qty: qty})
		}
		if !validUnits(check) {
			return nil, erp.ErrValidation
		}
		in := erp.SalesOrderInput{Company: x.binding.Company, Customer: args.Customer, TransactionDate: td, DeliveryDate: dd, Lines: lines}
		access := erp.DraftAccessInput{DocType: c.dt, Company: x.binding.Company, Customer: args.Customer, ItemCodes: []string{}}
		for _, line := range lines {
			access.ItemCodes = append(access.ItemCodes, line.ItemCode)
		}
		if err := x.checkDraft(ctx, access, as, delegate); err != nil {
			return nil, err
		}
		return func(ctx context.Context) (SafeResult, error) {
			if err := x.checkDraft(ctx, access, as, delegate); err != nil {
				return SafeResult{Code: "AUTHORITY_REVOKED", NeedsHuman: true}, nil
			}
			ref, err := x.port.CreateSalesOrder(ctx, as, in)
			return draftResult(c.dt, ref, err)
		}, nil
	}
	var args struct {
		Name        string `json:"name"`
		PostingDate string `json:"posting_date"`
		Mode        string `json:"mode_of_payment"`
	}
	if !c.write {
		var read struct {
			Name string `json:"name"`
		}
		if err := decodeArguments(b, &read); err != nil {
			return nil, erp.ErrValidation
		}
		args.Name = read.Name
	} else if c.name == "create_payment_draft" {
		if err := decodeArguments(b, &args); err != nil || !validName(args.Mode) {
			return nil, erp.ErrValidation
		}
	} else {
		var source struct {
			Name        string `json:"name"`
			PostingDate string `json:"posting_date"`
		}
		if err := decodeArguments(b, &source); err != nil {
			return nil, erp.ErrValidation
		}
		args.Name, args.PostingDate = source.Name, source.PostingDate
	}
	if !validName(args.Name) {
		return nil, erp.ErrValidation
	}
	var posting *erp.Date
	if args.PostingDate != "" {
		date, err := erp.ParseDate(args.PostingDate)
		if err != nil {
			return nil, erp.ErrValidation
		}
		posting = &date
	}
	sourceDT := c.dt
	if c.name == "create_delivery_note_draft" {
		sourceDT = erp.DocSalesOrder
	}
	if c.name == "create_sales_invoice_draft" {
		sourceDT = erp.DocDeliveryNote
	}
	if c.name == "create_payment_draft" {
		sourceDT = erp.DocSalesInvoice
	}
	// Read as BOTH users: the delegated principal must have access to the actual
	// source document, not just doctype permission or company membership.
	doc, err := x.read(ctx, delegate, sourceDT, args.Name)
	if err != nil {
		return nil, err
	}
	doc, err = x.read(ctx, as, sourceDT, args.Name)
	if err != nil {
		return nil, err
	}
	if !c.write {
		return func(ctx context.Context) (SafeResult, error) {
			if _, err := x.read(ctx, delegate, sourceDT, args.Name); err != nil {
				return SafeResult{Code: "AUTHORITY_REVOKED", NeedsHuman: true}, nil
			}
			fresh, err := x.read(ctx, as, sourceDT, args.Name)
			if err != nil {
				return SafeResult{Code: "AUTHORITY_REVOKED", NeedsHuman: true}, nil
			}
			return SafeResult{Ref: fresh.ref, Status: fresh.status, Code: "OK"}, nil
		}, nil
	}
	if doc.status != erp.DocSubmitted || !validUnits(doc.lines) {
		return nil, erp.ErrValidation
	}
	if c.name == "create_payment_draft" && (doc.isReturn || doc.outstanding.Cmp(erp.Decimal{}) <= 0 || doc.total.Cmp(erp.Decimal{}) <= 0 || doc.outstanding.Cmp(doc.total) > 0) {
		return nil, erp.ErrValidation
	}
	access := erp.DraftAccessInput{DocType: c.dt, Company: x.binding.Company, Customer: doc.customer, ModeOfPayment: args.Mode, ItemCodes: []string{}}
	for _, line := range doc.lines {
		access.ItemCodes = append(access.ItemCodes, line.ItemCode)
	}
	if err := x.checkDraft(ctx, access, as, delegate); err != nil {
		return nil, err
	}
	return func(ctx context.Context) (SafeResult, error) {
		freshDelegate, err := x.read(ctx, delegate, sourceDT, args.Name)
		if err != nil {
			return SafeResult{Code: "AUTHORITY_REVOKED", NeedsHuman: true}, nil
		}
		freshAgent, err := x.read(ctx, as, sourceDT, args.Name)
		if err != nil {
			return SafeResult{Code: "AUTHORITY_REVOKED", NeedsHuman: true}, nil
		}
		if !samePolicySource(doc, freshDelegate) || !samePolicySource(doc, freshAgent) {
			return SafeResult{Code: "SOURCE_CHANGED", NeedsHuman: true}, nil
		}
		if err := x.checkDraft(ctx, access, as, delegate); err != nil {
			return SafeResult{Code: "AUTHORITY_REVOKED", NeedsHuman: true}, nil
		}
		var ref erp.DocRef
		err = nil
		switch c.name {
		case "create_delivery_note_draft":
			ref, err = x.port.CreateDeliveryNote(ctx, as, doc.ref, posting)
		case "create_sales_invoice_draft":
			ref, err = x.port.CreateSalesInvoice(ctx, as, doc.ref, posting)
		case "create_payment_draft":
			ref, err = x.port.CreatePayment(ctx, as, erp.PaymentInput{Invoice: doc.ref.Name, ModeOfPayment: args.Mode, PostingDate: posting})
		}
		return draftResult(c.dt, ref, err)
	}, nil
}

func draftResult(dt erp.DocType, ref erp.DocRef, err error) (SafeResult, error) {
	if err != nil {
		return SafeResult{}, err
	}
	if ref.DocType != dt || !validName(ref.Name) {
		return SafeResult{}, erp.ErrUnavailable
	}
	return SafeResult{Ref: ref, Status: erp.DocDraft, Code: "OK"}, nil
}

type policyDocument struct {
	customer           string
	ref                erp.DocRef
	status             erp.DocStatus
	lines              []erp.Line
	outstanding, total erp.Decimal
	isReturn           bool
}

// Reusing a source name is not approval for changed quantities, parties or
// settlement facts. Compare policy-relevant state immediately before the effect.
func samePolicySource(a, b policyDocument) bool {
	if a.ref != b.ref || a.status != b.status || a.customer != b.customer || a.isReturn != b.isReturn || a.total.Cmp(b.total) != 0 || a.outstanding.Cmp(b.outstanding) != 0 || len(a.lines) != len(b.lines) {
		return false
	}
	for i, line := range a.lines {
		if line.ItemCode != b.lines[i].ItemCode || line.Qty.Cmp(b.lines[i].Qty) != 0 {
			return false
		}
	}
	return true
}

func (x *PolicyExecutor) read(ctx context.Context, as erp.Principal, dt erp.DocType, name string) (policyDocument, error) {
	var out policyDocument
	var company string
	var err error
	switch dt {
	case erp.DocSalesOrder:
		v, e := x.port.GetSalesOrder(ctx, as, name)
		err = e
		company = v.Company
		out = policyDocument{ref: v.Ref, status: v.Status, lines: v.Lines, customer: v.Customer}
	case erp.DocDeliveryNote:
		v, e := x.port.GetDeliveryNote(ctx, as, name)
		err = e
		company = v.Company
		out = policyDocument{ref: v.Ref, status: v.Status, lines: v.Lines, customer: v.Customer}
	case erp.DocSalesInvoice:
		v, e := x.port.GetSalesInvoice(ctx, as, name)
		err = e
		company = v.Company
		out = policyDocument{ref: v.Ref, status: v.Status, lines: v.Lines, customer: v.Customer, outstanding: v.Outstanding, total: v.RoundedTotal, isReturn: v.IsReturn}
	case erp.DocPaymentEntry:
		v, e := x.port.GetPaymentEntry(ctx, as, name)
		err = e
		company = v.Company
		out = policyDocument{ref: v.Ref, status: v.Status}
	default:
		return out, erp.ErrValidation
	}
	if err != nil {
		return out, err
	}
	if company != x.binding.Company || out.ref.DocType != dt || out.ref.Name != name || !slices.Contains([]erp.DocStatus{erp.DocDraft, erp.DocSubmitted, erp.DocCancelled}, out.status) {
		return policyDocument{}, erp.ErrForbidden
	}
	return out, nil
}

func (x *PolicyExecutor) checkDraft(ctx context.Context, in erp.DraftAccessInput, as, delegate erp.Principal) error {
	if err := x.port.CheckDraftAccess(ctx, delegate.Credential, in); err != nil {
		return err
	}
	return x.port.CheckDraftAccess(ctx, as.Credential, in)
}

// Canonicalization preserves decimal text, rejects duplicate keys at every
// depth, bounds recursion, and gives durable intents a stable key order.
func canonicalArguments(b []byte) ([]byte, error) {
	if len(b) > 64*1024 || !jsonObject(b) {
		return nil, erp.ErrValidation
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	nodes := 0
	var parse func(int) (any, error)
	parse = func(depth int) (any, error) {
		nodes++
		if depth > 16 || nodes > 4096 {
			return nil, erp.ErrValidation
		}
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				m := map[string]any{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return nil, err
					}
					s, ok := key.(string)
					if !ok {
						return nil, erp.ErrValidation
					}
					if _, exists := m[s]; exists {
						return nil, erp.ErrValidation
					}
					v, err := parse(depth + 1)
					if err != nil {
						return nil, err
					}
					m[s] = v
				}
				_, err = d.Token()
				return m, err
			case '[':
				a := []any{}
				for d.More() {
					v, err := parse(depth + 1)
					if err != nil {
						return nil, err
					}
					a = append(a, v)
				}
				_, err = d.Token()
				return a, err
			default:
				return nil, erp.ErrValidation
			}
		}
		if t == nil {
			return nil, erp.ErrValidation
		}
		return t, nil
	}
	value, err := parse(0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, erp.ErrValidation
	}
	return json.Marshal(value)
}
