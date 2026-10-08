// Package erp is the domain boundary to the ERP (ERPNext today, a Go core in
// phase 2). Go code reaches the ERP only through Port; every call carries the
// caller's Principal and credential.
package erp

// Credential proves who a Principal is to the ERP.
type Credential interface{ credential() }

// SessionCredential is a browser session (cookie sid) plus its CSRF token.
type SessionCredential struct{ SessionID, CSRFToken string }

// APIKeyCredential is a per-user API key and secret.
type APIKeyCredential struct{ Key, Secret string }

func (SessionCredential) credential() {}
func (APIKeyCredential) credential()  {}

// PrincipalKind classifies who acts.
type PrincipalKind string

const (
	PrincipalUser        PrincipalKind = "user"
	PrincipalAgent       PrincipalKind = "agent"
	PrincipalSystem      PrincipalKind = "system"
	PrincipalIntegration PrincipalKind = "integration"
)

// Principal is the acting identity of a Port call. OnBehalfOf is sent to the
// ERP as header X-Town-On-Behalf-Of.
type Principal struct {
	Kind       PrincipalKind
	ID         string
	OnBehalfOf string
	Roles      []string
	Companies  []string
	Credential Credential
}

// Identity is what the ERP reports for a credential (whoami).
type Identity struct {
	User       string
	Roles      []string
	Companies  []string
	Credential Credential
}
