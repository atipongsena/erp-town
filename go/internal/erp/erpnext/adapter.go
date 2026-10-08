package erpnext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Adapter implements erp.Port on ERPNext v16 through Client. Every call uses
// the calling principal's own credential; the adapter holds none of its own.
//
// It caches two things that are the same for every caller: the Company list
// (abbr <-> name; see companyCache) and each doctype's workflow (states and
// their docstatus, for workflowTTL). Both are read with the credential of the
// caller that needed them.
type Adapter struct {
	c *Client

	companies companyCache

	workflowMu sync.Mutex
	workflows  map[erp.DocType]cachedWorkflow
}

var _ erp.Port = (*Adapter)(nil)

// NewAdapter builds an Adapter on c.
func NewAdapter(c *Client) *Adapter {
	return &Adapter{
		c: c,
		companies: companyCache{
			byAbbr: map[string]string{},
			byName: map[string]string{},
			misses: map[companyMiss]time.Time{},
		},
		workflows: map[erp.DocType]cachedWorkflow{},
	}
}

// --- companies ---

// companyMissTTL: after a reload that did not find a key, the same principal's
// lookups of that key answer "unknown" without reloading for this long.
const companyMissTTL = 5 * time.Second

type companyRow struct {
	Name string `json:"name"`
	Abbr string `json:"abbr"`
}

// companyCache maps Company abbr <-> name. Each load is the loading caller's
// permission-filtered view; rows are merged (an abbr/name pair never changes
// meaning), so callers with different views do not evict each other. One load
// runs at a time, outside the lock; concurrent lookups wait for it under their
// own context and share its failure. A key still unknown after the caller's
// own load is remembered per principal for companyMissTTL.
type companyCache struct {
	mu     sync.Mutex
	byAbbr map[string]string // abbr -> company name
	byName map[string]string // company name -> abbr
	flight *companyFlight    // the load in progress, if any
	misses map[companyMiss]time.Time
}

type companyFlight struct {
	done chan struct{} // closed when the load ends
	err  error         // set before done is closed
}

type companyMiss struct {
	who, key string
	byName   bool
}

func (cc *companyCache) get(key string, byName bool) (string, bool) {
	if byName {
		v, ok := cc.byName[key]
		return v, ok
	}
	v, ok := cc.byAbbr[key]
	return v, ok
}

func (cc *companyCache) merge(rows []companyRow) {
	for _, r := range rows {
		if r.Name == "" || r.Abbr == "" {
			continue
		}
		cc.byAbbr[r.Abbr] = r.Name
		cc.byName[r.Name] = r.Abbr
	}
}

func (cc *companyCache) rememberMiss(m companyMiss, now time.Time) {
	for k, until := range cc.misses {
		if !now.Before(until) {
			delete(cc.misses, k)
		}
	}
	cc.misses[m] = now.Add(companyMissTTL)
}

func (a *Adapter) fetchCompanies(ctx context.Context, as erp.Principal) ([]companyRow, error) {
	var rows []companyRow
	q := url.Values{"fields": {`["name","abbr"]`}, "limit_page_length": {"0"}}
	if err := a.c.Get(ctx, as, ResourcePath("Company", ""), q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// lookupCompany resolves key (an abbr, or a name when byName) from the cache,
// loading the caller's view of the Company list when the key is missing.
// ok is false for an unknown key.
func (a *Adapter) lookupCompany(ctx context.Context, as erp.Principal, key string, byName bool) (string, bool, error) {
	cc := &a.companies
	miss := companyMiss{who: as.ID, key: key, byName: byName}
	for {
		cc.mu.Lock()
		if v, ok := cc.get(key, byName); ok {
			cc.mu.Unlock()
			return v, true, nil
		}
		if until, ok := cc.misses[miss]; ok && time.Now().Before(until) {
			cc.mu.Unlock()
			return "", false, nil
		}
		if fl := cc.flight; fl != nil {
			cc.mu.Unlock()
			select {
			case <-fl.done:
			case <-ctx.Done():
				return "", false, fmt.Errorf("erpnext: waiting for the company list: %w", ctx.Err())
			}
			if fl.err != nil && !errors.Is(fl.err, context.Canceled) && !errors.Is(fl.err, context.DeadlineExceeded) {
				return "", false, fl.err
			}
			continue // the key was not in that caller's view, or that caller gave up: look again
		}
		fl := &companyFlight{done: make(chan struct{})}
		cc.flight = fl
		cc.mu.Unlock()

		rows, err := a.fetchCompanies(ctx, as)

		cc.mu.Lock()
		cc.flight = nil
		fl.err = err
		close(fl.done)
		if err != nil {
			cc.mu.Unlock()
			return "", false, err
		}
		cc.merge(rows)
		v, ok := cc.get(key, byName)
		if !ok {
			cc.rememberMiss(miss, time.Now())
		}
		cc.mu.Unlock()
		return v, ok, nil
	}
}

// companyName maps a Company abbr (the domain's company code) to ERPNext's
// Company name; an unknown abbr is a ValidationError on "company".
func (a *Adapter) companyName(ctx context.Context, as erp.Principal, abbr string) (string, error) {
	name, ok, err := a.lookupCompany(ctx, as, abbr, false)
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
		abbr, ok, err := a.lookupCompany(ctx, as, name, true)
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
