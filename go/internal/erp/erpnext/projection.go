package erpnext

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

var _ erp.Projection = (*Adapter)(nil)

func (a *Adapter) ReadProjection(ctx context.Context, as erp.Principal, company string) (erp.ProjectionSnapshot, error) {
	if err := erp.ValidateProjectionPrincipal(as, company); err != nil {
		return erp.ProjectionSnapshot{}, err
	}
	var raw json.RawMessage
	if err := a.c.Get(ctx, as, "/api/method/erp_town.projection_api.read_projection", url.Values{"company": {company}}, &raw); err != nil {
		return erp.ProjectionSnapshot{}, err
	}
	var response struct {
		Company string `json:"company"`
		Facts   []struct {
			DocType     erp.DocType  `json:"doctype"`
			Name        string       `json:"name"`
			Kind        string       `json:"kind"`
			Scope       string       `json:"scope,omitempty"`
			Status      string       `json:"status,omitempty"`
			FillRatio   *erp.Decimal `json:"fill_ratio,omitempty"`
			DueDate     *erp.Date    `json:"due_date,omitempty"`
			DaysOverdue *int         `json:"days_overdue,omitempty"`
		} `json:"facts"`
		Sequences []struct {
			DocType erp.DocType `json:"doctype"`
			Name    string      `json:"name"`
			Seq     int64       `json:"seq"`
		} `json:"sequences"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return erp.ProjectionSnapshot{}, fmt.Errorf("erpnext: invalid projection response: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return erp.ProjectionSnapshot{}, fmt.Errorf("erpnext: trailing projection response")
	}
	out := erp.ProjectionSnapshot{Company: response.Company}
	if response.Facts != nil {
		out.Facts = []erp.ProjectionFact{}
	}
	if response.Sequences != nil {
		out.Sequences = []erp.ProjectionSequence{}
	}
	for _, row := range response.Facts {
		fact := erp.ProjectionFact{Ref: erp.DocRef{DocType: row.DocType, Name: row.Name}, Kind: row.Kind, Scope: row.Scope, Status: row.Status}
		if row.Kind == "stock" && row.FillRatio == nil || row.Kind == "overdue" && (row.DueDate == nil || row.DaysOverdue == nil) {
			return erp.ProjectionSnapshot{}, fmt.Errorf("erpnext: incomplete projection fact")
		}
		if row.FillRatio != nil {
			fact.FillRatio = *row.FillRatio
		}
		if row.DueDate != nil {
			fact.DueDate = *row.DueDate
		}
		if row.DaysOverdue != nil {
			fact.DaysOverdue = *row.DaysOverdue
		}
		out.Facts = append(out.Facts, fact)
	}
	for _, row := range response.Sequences {
		out.Sequences = append(out.Sequences, erp.ProjectionSequence{Ref: erp.DocRef{DocType: row.DocType, Name: row.Name}, Seq: row.Seq})
	}
	return out, out.Validate(company)
}
