package erp

import "context"

// AgentAccess is a fresh ERP authorization snapshot, never provider input.
// An empty AgentKey means the authenticated user has no enabled agent identity.
type AgentAccess struct {
	Identity    Identity
	AgentKey    string
	Permissions map[DocType]DocumentPermission
}

type DocumentPermission struct{ Read, Create bool }

// DraftAccessInput checks referenced master records under the current user's
// ERP permissions. It contains no prices and cannot grant delegation.
type DraftAccessInput struct {
	DocType       DocType  `json:"doctype"`
	Company       string   `json:"company"`
	Customer      string   `json:"customer"`
	ItemCodes     []string `json:"item_codes"`
	ModeOfPayment string   `json:"mode_of_payment"`
}

// AgentIdentities checks live account, company and doctype permissions. It does
// not grant delegation; the caller must also authenticate the delegating user.
type AgentIdentities interface {
	AgentAccess(context.Context, Credential) (AgentAccess, error)
	CheckDraftAccess(context.Context, Credential, DraftAccessInput) error
}
