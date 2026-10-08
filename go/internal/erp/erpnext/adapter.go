package erpnext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Adapter implements erp.Port on ERPNext v16 through Client. Every call uses
// the calling principal's own credential; the adapter holds none of its own.
//
// It caches two things that are the same for every caller: the Company list
// (abbr <-> name, reloaded once on a miss) and each doctype's workflow
// (states and their docstatus, for workflowTTL). Both are read with the
// credential of the caller that needed them.
type Adapter struct {
	c *Client

	companyMu sync.Mutex
	byAbbr    map[string]string // abbr -> company name
	byName    map[string]string // company name -> abbr

	workflowMu sync.Mutex
	workflows  map[erp.DocType]cachedWorkflow
}

var _ erp.Port = (*Adapter)(nil)

// NewAdapter builds an Adapter on c.
func NewAdapter(c *Client) *Adapter {
	return &Adapter{c: c, workflows: map[erp.DocType]cachedWorkflow{}}
}

// --- companies ---

type companyRow struct {
	Name string `json:"name"`
	Abbr string `json:"abbr"`
}

func (a *Adapter) loadCompanies(ctx context.Context, as erp.Principal) error {
	var rows []companyRow
	q := url.Values{"fields": {`["name","abbr"]`}, "limit_page_length": {"0"}}
	if err := a.c.Get(ctx, as, ResourcePath("Company", ""), q, &rows); err != nil {
		return err
	}
	byAbbr := make(map[string]string, len(rows))
	byName := make(map[string]string, len(rows))
	for _, r := range rows {
		byAbbr[r.Abbr] = r.Name
		byName[r.Name] = r.Abbr
	}
	a.byAbbr, a.byName = byAbbr, byName
	return nil
}

// lookupCompany resolves key in the map pick returns, loading the list when it
// is missing and reloading it once on a miss. ok is false for an unknown key.
func (a *Adapter) lookupCompany(ctx context.Context, as erp.Principal, key string, pick func() map[string]string) (string, bool, error) {
	a.companyMu.Lock()
	defer a.companyMu.Unlock()
	if a.byAbbr != nil {
		if v, ok := pick()[key]; ok {
			return v, true, nil
		}
	}
	if err := a.loadCompanies(ctx, as); err != nil {
		return "", false, err
	}
	v, ok := pick()[key]
	return v, ok, nil
}

// companyName maps a Company abbr (the domain's company code) to ERPNext's
// Company name; an unknown abbr is a ValidationError on "company".
func (a *Adapter) companyName(ctx context.Context, as erp.Principal, abbr string) (string, error) {
	name, ok, err := a.lookupCompany(ctx, as, abbr, func() map[string]string { return a.byAbbr })
	if err != nil {
		return "", err
	}
	if !ok {
		return "", erp.NewValidation("company", "unknown company "+abbr)
	}
	return name, nil
}

// companyAbbr maps an ERPNext Company name back to its abbr.
func (a *Adapter) companyAbbr(ctx context.Context, as erp.Principal) func(string) (string, error) {
	return func(name string) (string, error) {
		abbr, ok, err := a.lookupCompany(ctx, as, name, func() map[string]string { return a.byName })
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("erpnext: company %q is not in the company list", name)
		}
		return abbr, nil
	}
}

// --- documents ---

// rawDoc is an ERPNext document passed back as received (numbers untouched).
type rawDoc map[string]json.RawMessage

func (d rawDoc) str(field string) string {
	var s string
	if raw, ok := d[field]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

func (d rawDoc) docstatus() int {
	var n int
	if raw, ok := d["docstatus"]; ok {
		_ = json.Unmarshal(raw, &n)
	}
	return n
}

func (d rawDoc) set(field string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("erpnext: encode %s: %w", field, err)
	}
	d[field] = b
	return nil
}

func (a *Adapter) getDoc(ctx context.Context, as erp.Principal, ref erp.DocRef, out any) error {
	return a.c.Get(ctx, as, ResourcePath(string(ref.DocType), ref.Name), nil, out)
}

// insert creates doc and returns its reference.
func (a *Adapter) insert(ctx context.Context, as erp.Principal, dt erp.DocType, doc any) (erp.DocRef, error) {
	var created struct {
		Name string `json:"name"`
	}
	if err := a.c.Insert(ctx, as, string(dt), doc, &created); err != nil {
		return erp.DocRef{}, err
	}
	if created.Name == "" {
		return erp.DocRef{}, fmt.Errorf("erpnext: %s insert returned no name", dt)
	}
	return erp.DocRef{DocType: dt, Name: created.Name}, nil
}

// setPosting makes a draft from a make_* mapper use date as its posting date.
func setPosting(d rawDoc, date *erp.Date) error {
	if date == nil || date.IsZero() {
		return nil
	}
	if err := d.set("set_posting_time", 1); err != nil {
		return err
	}
	return d.set("posting_date", date.String())
}

func docStatus(n int) (erp.DocStatus, error) {
	switch n {
	case 0:
		return erp.DocDraft, nil
	case 1:
		return erp.DocSubmitted, nil
	case 2:
		return erp.DocCancelled, nil
	}
	return "", fmt.Errorf("erpnext: unknown docstatus %d", n)
}

// lineRow is the part of an item row the domain reads.
type lineRow struct {
	ItemCode string      `json:"item_code"`
	Qty      erp.Decimal `json:"qty"`
	Rate     erp.Decimal `json:"rate"`
	Amount   erp.Decimal `json:"amount"`
}

func mapLines(rows []lineRow) []erp.Line {
	out := make([]erp.Line, 0, len(rows))
	for _, r := range rows {
		out = append(out, erp.Line{ItemCode: r.ItemCode, Qty: r.Qty, Rate: r.Rate, Amount: r.Amount})
	}
	return out
}

// payable is the rounded total, or the grand total when the document has
// rounding disabled (ERPNext then stores rounded_total 0).
func payable(grand, rounded erp.Decimal, disabled int) erp.Decimal {
	if disabled != 0 {
		return grand
	}
	return rounded
}

// appendUnique appends s unless it is empty or already present.
func appendUnique(list []string, s string) []string {
	if s == "" {
		return list
	}
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// notFoundAsValidation turns ERPNext's DoesNotExistError on a referenced
// record into a ValidationError on field.
func notFoundAsValidation(err error, field string) error {
	if errors.Is(err, erp.ErrNotFound) {
		return erp.NewValidation(field, errorText(err))
	}
	return err
}

// errorText strips the domain sentinel prefix from a mapped error.
func errorText(err error) string {
	msg := err.Error()
	for _, sentinel := range []error{erp.ErrNotFound, erp.ErrForbidden, erp.ErrValidation} {
		prefix := sentinel.Error() + ": "
		if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
			return msg[len(prefix):]
		}
	}
	return msg
}
