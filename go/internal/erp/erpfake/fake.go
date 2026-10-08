// Package erpfake is an in-memory erp.Port that models the ERPNext seed's
// behaviour (VAT, roles, credit check, Sales Order approval workflow) closely
// enough for the shared porttest suite and for tests of the layers above.
package erpfake

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Role names, as in the seed.
const (
	RoleSalesUser    = "Sales User"
	RoleSalesManager = "Sales Manager"
	RoleStockUser    = "Stock User"
	RoleAccountsUser = "Accounts User"
)

// FakeUser is one logged-in session.
type FakeUser struct {
	User      string
	Roles     []string
	Companies []string
	CSRFToken string
}

// Config seeds the fake. Users is keyed by session id.
type Config struct {
	Company      string
	Today        erp.Date
	Prices       map[string]erp.Decimal
	CreditLimits map[string]erp.Decimal
	Users        map[string]FakeUser
}

// Fake implements erp.Port in memory.
type Fake struct {
	cfg Config

	mu  sync.Mutex
	seq map[erp.DocType]int
	so  map[string]*salesOrder
	dn  map[string]*deliveryNote
	si  map[string]*salesInvoice
	pe  map[string]*paymentEntry
}

type (
	salesOrder   struct{ erp.SalesOrder }
	deliveryNote struct{ erp.DeliveryNote }
	salesInvoice struct {
		erp.SalesInvoice
		deliveryNote string
	}
)
type paymentEntry struct{ erp.PaymentEntry }

var _ erp.Port = (*Fake)(nil)

// New builds an empty fake.
func New(cfg Config) *Fake {
	if cfg.Today.IsZero() {
		cfg.Today = erp.MustDate("2026-10-09")
	}
	return &Fake{
		cfg: cfg,
		seq: map[erp.DocType]int{},
		so:  map[string]*salesOrder{},
		dn:  map[string]*deliveryNote{},
		si:  map[string]*salesInvoice{},
		pe:  map[string]*paymentEntry{},
	}
}

var vatRate = erp.MustDecimal("0.07")

var prefixes = map[erp.DocType]string{
	erp.DocSalesOrder:   "SAL-ORD",
	erp.DocDeliveryNote: "MAT-DN",
	erp.DocSalesInvoice: "ACC-SINV",
	erp.DocPaymentEntry: "ACC-PAY",
}

// --- auth ---

func (f *Fake) user(cred erp.Credential) (FakeUser, error) {
	sc, ok := cred.(erp.SessionCredential)
	if !ok {
		return FakeUser{}, erp.ErrUnauthenticated
	}
	u, ok := f.cfg.Users[sc.SessionID]
	if !ok {
		return FakeUser{}, erp.ErrUnauthenticated
	}
	return u, nil
}

func (u FakeUser) has(roles ...string) bool {
	return slices.ContainsFunc(roles, func(r string) bool { return slices.Contains(u.Roles, r) })
}

// authorize resolves the caller and requires one of roles (none = any ERP role).
func (f *Fake) authorize(as erp.Principal, roles ...string) (FakeUser, error) {
	u, err := f.user(as.Credential)
	if err != nil {
		return FakeUser{}, err
	}
	if !slices.Contains(u.Companies, f.cfg.Company) {
		return FakeUser{}, fmt.Errorf("%w: no access to company %s", erp.ErrForbidden, f.cfg.Company)
	}
	if len(roles) == 0 {
		roles = []string{RoleSalesUser, RoleSalesManager, RoleStockUser, RoleAccountsUser}
	}
	if !u.has(roles...) {
		return FakeUser{}, fmt.Errorf("%w: %s needs one of %v", erp.ErrForbidden, u.User, roles)
	}
	return u, nil
}

func writeRoles(dt erp.DocType) []string {
	switch dt {
	case erp.DocSalesOrder:
		return []string{RoleSalesUser, RoleSalesManager}
	case erp.DocDeliveryNote:
		return []string{RoleStockUser}
	default:
		return []string{RoleAccountsUser}
	}
}

func (f *Fake) nextName(dt erp.DocType) string {
	f.seq[dt]++
	return fmt.Sprintf("%s-%05d", prefixes[dt], f.seq[dt])
}

func (f *Fake) date(d *erp.Date) erp.Date {
	if d != nil && !d.IsZero() {
		return *d
	}
	return f.cfg.Today
}

func cloneLines(l []erp.Line) []erp.Line { return slices.Clone(l) }

func notFound(dt erp.DocType, name string) error {
	return fmt.Errorf("%w: %s %s", erp.ErrNotFound, dt, name)
}

// --- Sales ---

func (f *Fake) CreateSalesOrder(_ context.Context, as erp.Principal, in erp.SalesOrderInput) (erp.DocRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as, writeRoles(erp.DocSalesOrder)...); err != nil {
		return erp.DocRef{}, err
	}
	if in.Company != f.cfg.Company {
		return erp.DocRef{}, erp.NewValidation("company", "unknown company "+in.Company)
	}
	if !f.cfg.knownCustomer(in.Customer) {
		return erp.DocRef{}, erp.NewValidation("customer", "unknown customer "+in.Customer)
	}
	if in.TransactionDate.IsZero() || in.DeliveryDate.IsZero() {
		return erp.DocRef{}, erp.NewValidation("dates", "transaction and delivery date are required")
	}
	if in.DeliveryDate.Before(in.TransactionDate) {
		return erp.DocRef{}, erp.NewValidation("delivery_date", "delivery date is before transaction date")
	}
	if len(in.Lines) == 0 {
		return erp.DocRef{}, erp.NewValidation("items", "at least one line is required")
	}
	lines := make([]erp.Line, 0, len(in.Lines))
	for _, li := range in.Lines {
		price, ok := f.cfg.Prices[li.ItemCode]
		if !ok {
			return erp.DocRef{}, erp.NewValidation("items", "unknown item "+li.ItemCode)
		}
		if li.Qty.Cmp(erp.Decimal{}) <= 0 {
			return erp.DocRef{}, erp.NewValidation("items", "quantity must be positive for "+li.ItemCode)
		}
		rate := price
		if li.Rate != nil {
			rate = *li.Rate
		}
		lines = append(lines, erp.Line{ItemCode: li.ItemCode, Qty: li.Qty, Rate: rate, Amount: li.Qty.Mul(rate)})
	}
	net, tax, grand := totals(lines)
	name := f.nextName(erp.DocSalesOrder)
	ref := erp.DocRef{DocType: erp.DocSalesOrder, Name: name}
	f.so[name] = &salesOrder{erp.SalesOrder{
		Ref: ref, Company: in.Company, Customer: in.Customer,
		TransactionDate: in.TransactionDate, DeliveryDate: in.DeliveryDate,
		Lines: lines, NetTotal: net, TaxTotal: tax, GrandTotal: grand,
		Status: erp.DocDraft, WorkflowState: stateDraft,
	}}
	return ref, nil
}

func (c Config) knownCustomer(name string) bool {
	// The fake accepts CUST-001..CUST-010 as in the seed, plus any customer given a credit limit.
	if _, ok := c.CreditLimits[name]; ok {
		return true
	}
	var n int
	_, err := fmt.Sscanf(name, "CUST-%03d", &n)
	return err == nil && n >= 1 && n <= 10 && fmt.Sprintf("CUST-%03d", n) == name
}

func totals(lines []erp.Line) (net, tax, grand erp.Decimal) {
	for _, l := range lines {
		net = net.Add(l.Amount)
	}
	tax = net.Mul(vatRate).RoundTo(2)
	return net, tax, net.Add(tax)
}

func (f *Fake) GetSalesOrder(_ context.Context, as erp.Principal, name string) (erp.SalesOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as); err != nil {
		return erp.SalesOrder{}, err
	}
	d, ok := f.so[name]
	if !ok {
		return erp.SalesOrder{}, notFound(erp.DocSalesOrder, name)
	}
	out := d.SalesOrder
	out.Lines = cloneLines(out.Lines)
	return out, nil
}

// --- Delivery ---

func (f *Fake) CreateDeliveryNote(_ context.Context, as erp.Principal, so erp.DocRef, postingDate *erp.Date) (erp.DocRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as, writeRoles(erp.DocDeliveryNote)...); err != nil {
		return erp.DocRef{}, err
	}
	src, err := f.lookupSO(so)
	if err != nil {
		return erp.DocRef{}, err
	}
	if src.Status != erp.DocSubmitted {
		return erp.DocRef{}, erp.NewValidation("sales_order", "sales order is not submitted")
	}
	name := f.nextName(erp.DocDeliveryNote)
	ref := erp.DocRef{DocType: erp.DocDeliveryNote, Name: name}
	f.dn[name] = &deliveryNote{DeliveryNote: erp.DeliveryNote{
		Ref: ref, Company: src.Company, Customer: src.Customer, PostingDate: f.date(postingDate),
		Lines: cloneLines(src.Lines), Status: erp.DocDraft, AgainstSalesOrders: []string{src.Ref.Name},
	}}
	return ref, nil
}

func (f *Fake) lookupSO(ref erp.DocRef) (*salesOrder, error) {
	if ref.DocType != erp.DocSalesOrder {
		return nil, erp.NewValidation("sales_order", "reference is not a Sales Order")
	}
	d, ok := f.so[ref.Name]
	if !ok {
		return nil, notFound(ref.DocType, ref.Name)
	}
	return d, nil
}

func (f *Fake) GetDeliveryNote(_ context.Context, as erp.Principal, name string) (erp.DeliveryNote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as); err != nil {
		return erp.DeliveryNote{}, err
	}
	d, ok := f.dn[name]
	if !ok {
		return erp.DeliveryNote{}, notFound(erp.DocDeliveryNote, name)
	}
	out := d.DeliveryNote
	out.Lines = cloneLines(out.Lines)
	out.AgainstSalesOrders = slices.Clone(out.AgainstSalesOrders)
	return out, nil
}

// --- Billing ---

func (f *Fake) CreateSalesInvoice(_ context.Context, as erp.Principal, dn erp.DocRef, postingDate *erp.Date) (erp.DocRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as, writeRoles(erp.DocSalesInvoice)...); err != nil {
		return erp.DocRef{}, err
	}
	if dn.DocType != erp.DocDeliveryNote {
		return erp.DocRef{}, erp.NewValidation("delivery_note", "reference is not a Delivery Note")
	}
	src, ok := f.dn[dn.Name]
	if !ok {
		return erp.DocRef{}, notFound(dn.DocType, dn.Name)
	}
	if src.Status != erp.DocSubmitted {
		return erp.DocRef{}, erp.NewValidation("delivery_note", "delivery note is not submitted")
	}
	posting := f.date(postingDate)
	net, tax, grand := totals(src.Lines)
	name := f.nextName(erp.DocSalesInvoice)
	ref := erp.DocRef{DocType: erp.DocSalesInvoice, Name: name}
	f.si[name] = &salesInvoice{deliveryNote: dn.Name, SalesInvoice: erp.SalesInvoice{
		Ref: ref, Company: src.Company, Customer: src.Customer,
		PostingDate: posting, DueDate: posting.AddDays(30),
		Lines: cloneLines(src.Lines), NetTotal: net, TaxTotal: tax, GrandTotal: grand, Outstanding: grand,
		Status: erp.DocDraft,
	}}
	return ref, nil
}

func (f *Fake) GetSalesInvoice(_ context.Context, as erp.Principal, name string) (erp.SalesInvoice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as); err != nil {
		return erp.SalesInvoice{}, err
	}
	d, ok := f.si[name]
	if !ok {
		return erp.SalesInvoice{}, notFound(erp.DocSalesInvoice, name)
	}
	out := d.SalesInvoice
	out.Lines = cloneLines(out.Lines)
	return out, nil
}

func (f *Fake) CreatePayment(_ context.Context, as erp.Principal, in erp.PaymentInput) (erp.DocRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as, writeRoles(erp.DocPaymentEntry)...); err != nil {
		return erp.DocRef{}, err
	}
	if in.ModeOfPayment == "" {
		return erp.DocRef{}, erp.NewValidation("mode_of_payment", "mode of payment is required")
	}
	inv, ok := f.si[in.Invoice]
	if !ok {
		return erp.DocRef{}, notFound(erp.DocSalesInvoice, in.Invoice)
	}
	if inv.Status != erp.DocSubmitted {
		return erp.DocRef{}, erp.NewValidation("invoice", "invoice is not submitted")
	}
	if inv.Outstanding.Cmp(erp.Decimal{}) <= 0 {
		return erp.DocRef{}, erp.NewValidation("invoice", "invoice has nothing outstanding")
	}
	name := f.nextName(erp.DocPaymentEntry)
	ref := erp.DocRef{DocType: erp.DocPaymentEntry, Name: name}
	f.pe[name] = &paymentEntry{erp.PaymentEntry{
		Ref: ref, Company: inv.Company, Party: inv.Customer, PostingDate: f.date(in.PostingDate),
		Paid: inv.Outstanding, ModeOfPayment: in.ModeOfPayment, Status: erp.DocDraft,
		AgainstInvoices: []string{inv.Ref.Name},
	}}
	return ref, nil
}

func (f *Fake) GetPaymentEntry(_ context.Context, as erp.Principal, name string) (erp.PaymentEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as); err != nil {
		return erp.PaymentEntry{}, err
	}
	d, ok := f.pe[name]
	if !ok {
		return erp.PaymentEntry{}, notFound(erp.DocPaymentEntry, name)
	}
	out := d.PaymentEntry
	out.AgainstInvoices = slices.Clone(out.AgainstInvoices)
	return out, nil
}

// --- Documents ---

func (f *Fake) Submit(_ context.Context, as erp.Principal, ref erp.DocRef) (erp.SubmitResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.authorize(as, writeRoles(ref.DocType)...)
	if err != nil {
		return erp.SubmitResult{}, err
	}
	switch ref.DocType {
	case erp.DocSalesOrder:
		so, ok := f.so[ref.Name]
		if !ok {
			return erp.SubmitResult{}, notFound(ref.DocType, ref.Name)
		}
		if so.Status != erp.DocDraft {
			return erp.SubmitResult{}, fmt.Errorf("%w: sales order is %s", erp.ErrConflict, so.Status)
		}
		avail := f.transitions(u, so)
		var pick *transition
		for i := range avail {
			if avail[i].submits {
				pick = &avail[i]
				break
			}
		}
		if pick == nil && len(avail) == 1 {
			pick = &avail[0]
		}
		if pick == nil {
			return erp.SubmitResult{}, &erp.ValidationError{
				Message: "no workflow transition available",
				Fields:  map[string]string{"workflow": "no transition available from state " + so.WorkflowState},
			}
		}
		return f.apply(so, *pick), nil
	case erp.DocDeliveryNote:
		d, ok := f.dn[ref.Name]
		if !ok {
			return erp.SubmitResult{}, notFound(ref.DocType, ref.Name)
		}
		return erp.SubmitResult{Submitted: true}, submitDraft(&d.Status)
	case erp.DocSalesInvoice:
		d, ok := f.si[ref.Name]
		if !ok {
			return erp.SubmitResult{}, notFound(ref.DocType, ref.Name)
		}
		return erp.SubmitResult{Submitted: true}, submitDraft(&d.Status)
	case erp.DocPaymentEntry:
		d, ok := f.pe[ref.Name]
		if !ok {
			return erp.SubmitResult{}, notFound(ref.DocType, ref.Name)
		}
		if d.Status != erp.DocDraft {
			return erp.SubmitResult{}, fmt.Errorf("%w: payment entry is %s", erp.ErrConflict, d.Status)
		}
		inv := f.si[d.AgainstInvoices[0]]
		if inv.Status != erp.DocSubmitted || inv.Outstanding.Cmp(d.Paid) < 0 {
			return erp.SubmitResult{}, fmt.Errorf("%w: invoice %s can no longer take this payment", erp.ErrConflict, inv.Ref.Name)
		}
		inv.Outstanding = inv.Outstanding.Sub(d.Paid)
		d.Status = erp.DocSubmitted
		return erp.SubmitResult{Submitted: true}, nil
	}
	return erp.SubmitResult{}, erp.NewValidation("doctype", "unsupported doctype "+string(ref.DocType))
}

func submitDraft(s *erp.DocStatus) error {
	if *s != erp.DocDraft {
		return fmt.Errorf("%w: document is %s", erp.ErrConflict, *s)
	}
	*s = erp.DocSubmitted
	return nil
}

func (f *Fake) Cancel(_ context.Context, as erp.Principal, ref erp.DocRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.authorize(as, writeRoles(ref.DocType)...); err != nil {
		return err
	}
	var status *erp.DocStatus
	switch ref.DocType {
	case erp.DocSalesOrder:
		d, ok := f.so[ref.Name]
		if !ok {
			return notFound(ref.DocType, ref.Name)
		}
		status = &d.Status
		for _, dn := range f.dn {
			if dn.Status != erp.DocCancelled && slices.Contains(dn.AgainstSalesOrders, ref.Name) {
				return fmt.Errorf("%w: linked to delivery note %s", erp.ErrConflict, dn.Ref.Name)
			}
		}
	case erp.DocDeliveryNote:
		d, ok := f.dn[ref.Name]
		if !ok {
			return notFound(ref.DocType, ref.Name)
		}
		status = &d.Status
		for _, si := range f.si {
			if si.Status != erp.DocCancelled && si.deliveryNote == ref.Name {
				return fmt.Errorf("%w: linked to sales invoice %s", erp.ErrConflict, si.Ref.Name)
			}
		}
	case erp.DocSalesInvoice:
		d, ok := f.si[ref.Name]
		if !ok {
			return notFound(ref.DocType, ref.Name)
		}
		status = &d.Status
		for _, pe := range f.pe {
			if pe.Status != erp.DocCancelled && slices.Contains(pe.AgainstInvoices, ref.Name) {
				return fmt.Errorf("%w: linked to payment entry %s", erp.ErrConflict, pe.Ref.Name)
			}
		}
	case erp.DocPaymentEntry:
		d, ok := f.pe[ref.Name]
		if !ok {
			return notFound(ref.DocType, ref.Name)
		}
		status = &d.Status
		if d.Status == erp.DocSubmitted {
			inv := f.si[d.AgainstInvoices[0]]
			inv.Outstanding = inv.Outstanding.Add(d.Paid)
		}
	default:
		return erp.NewValidation("doctype", "unsupported doctype "+string(ref.DocType))
	}
	if *status != erp.DocSubmitted {
		if *status == erp.DocCancelled {
			return fmt.Errorf("%w: already cancelled", erp.ErrConflict)
		}
		return erp.NewValidation("docstatus", "only a submitted document can be cancelled")
	}
	*status = erp.DocCancelled
	return nil
}

// --- Workflow (Sales Order only) ---

const (
	stateDraft    = "Draft"
	statePending  = "รออนุมัติ"
	stateApproved = "อนุมัติแล้ว"
	stateRejected = "ปฏิเสธ"
)

type transition struct {
	action, to string
	submits    bool
}

// transitions lists what u may do on so right now.
func (f *Fake) transitions(u FakeUser, so *salesOrder) []transition {
	switch so.WorkflowState {
	case stateDraft:
		if !u.has(RoleSalesUser, RoleSalesManager) {
			return nil
		}
		if f.overCredit(so) {
			return []transition{{action: "ขออนุมัติ", to: statePending}}
		}
		return []transition{{action: "ยืนยัน", to: stateApproved, submits: true}}
	case statePending:
		if !u.has(RoleSalesManager) {
			return nil
		}
		return []transition{
			{action: "อนุมัติ", to: stateApproved, submits: true},
			{action: "ปฏิเสธ", to: stateRejected},
		}
	}
	return nil
}

// overCredit: grand total plus the customer's unpaid submitted invoices exceed its limit.
func (f *Fake) overCredit(so *salesOrder) bool {
	limit, ok := f.cfg.CreditLimits[so.Customer]
	if !ok {
		return false
	}
	unpaid := erp.Decimal{}
	for _, si := range f.si {
		if si.Customer == so.Customer && si.Status == erp.DocSubmitted {
			unpaid = unpaid.Add(si.Outstanding)
		}
	}
	return so.GrandTotal.Add(unpaid).Cmp(limit) > 0
}

func (f *Fake) apply(so *salesOrder, t transition) erp.SubmitResult {
	so.WorkflowState = t.to
	if t.submits {
		so.Status = erp.DocSubmitted
	}
	return erp.SubmitResult{Submitted: t.submits, WorkflowState: t.to}
}

func (f *Fake) Actions(_ context.Context, as erp.Principal, ref erp.DocRef) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.authorize(as)
	if err != nil {
		return nil, err
	}
	if ref.DocType != erp.DocSalesOrder {
		if _, err := f.exists(ref); err != nil {
			return nil, err
		}
		return []string{}, nil
	}
	so, ok := f.so[ref.Name]
	if !ok {
		return nil, notFound(ref.DocType, ref.Name)
	}
	out := []string{}
	if so.Status == erp.DocDraft {
		for _, t := range f.transitions(u, so) {
			out = append(out, t.action)
		}
	}
	return out, nil
}

func (f *Fake) exists(ref erp.DocRef) (bool, error) {
	var ok bool
	switch ref.DocType {
	case erp.DocDeliveryNote:
		_, ok = f.dn[ref.Name]
	case erp.DocSalesInvoice:
		_, ok = f.si[ref.Name]
	case erp.DocPaymentEntry:
		_, ok = f.pe[ref.Name]
	default:
		return false, erp.NewValidation("doctype", "unsupported doctype "+string(ref.DocType))
	}
	if !ok {
		return false, notFound(ref.DocType, ref.Name)
	}
	return true, nil
}

func (f *Fake) Apply(_ context.Context, as erp.Principal, ref erp.DocRef, action string) (erp.SubmitResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.authorize(as)
	if err != nil {
		return erp.SubmitResult{}, err
	}
	if ref.DocType != erp.DocSalesOrder {
		if _, err := f.exists(ref); err != nil {
			return erp.SubmitResult{}, err
		}
		return erp.SubmitResult{}, erp.NewValidation("workflow", string(ref.DocType)+" has no workflow")
	}
	so, ok := f.so[ref.Name]
	if !ok {
		return erp.SubmitResult{}, notFound(ref.DocType, ref.Name)
	}
	if so.Status == erp.DocDraft {
		for _, t := range f.transitions(u, so) {
			if t.action == action {
				return f.apply(so, t), nil
			}
		}
		// Known to the workflow but not to this principal -> forbidden; otherwise invalid.
		if f.knownAction(so, action) {
			return erp.SubmitResult{}, fmt.Errorf("%w: %s may not apply %q", erp.ErrForbidden, u.User, action)
		}
	}
	return erp.SubmitResult{}, &erp.ValidationError{
		Message: "action not available",
		Fields:  map[string]string{"workflow": fmt.Sprintf("%q is not available from state %s", action, so.WorkflowState)},
	}
}

// knownAction: is action a valid transition out of the order's state for some role?
func (f *Fake) knownAction(so *salesOrder, action string) bool {
	manager := FakeUser{Roles: []string{RoleSalesManager, RoleSalesUser}}
	return slices.ContainsFunc(f.transitions(manager, so), func(t transition) bool { return t.action == action })
}

// --- Identities ---

func (f *Fake) Whoami(_ context.Context, cred erp.Credential) (erp.Identity, error) {
	u, err := f.user(cred)
	if err != nil {
		return erp.Identity{}, err
	}
	return erp.Identity{
		User: u.User, Roles: slices.Clone(u.Roles), Companies: slices.Clone(u.Companies),
		Credential: erp.SessionCredential{SessionID: cred.(erp.SessionCredential).SessionID, CSRFToken: u.CSRFToken},
	}, nil
}
