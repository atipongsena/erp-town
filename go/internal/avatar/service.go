// Package avatar stores explicit appearance consent and ephemeral presence only.
// It has no business-write capability and no human activity journal.
package avatar

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

type (
	Preference struct {
		Consent    bool   `json:"consent"`
		Appearance string `json:"appearance"`
	}
	Presence struct {
		ID         string `json:"id"`
		Department string `json:"department"`
		Emote      string `json:"emote"`
		Appearance string `json:"appearance"`
	}
	liveEntry struct {
		Presence
		principal erp.Principal
		company   string
		expires   time.Time
	}
	userCompany struct{ user, company string }
	Service     struct {
		pool *pgxpool.Pool
		ids  erp.Identities
		now  func() time.Time
		mu   sync.Mutex
		live map[userCompany]liveEntry
	}
)

func NewService(pool *pgxpool.Pool, ids erp.Identities) *Service {
	return &Service{pool: pool, ids: ids, now: time.Now, live: map[userCompany]liveEntry{}}
}

func (s *Service) authority(ctx context.Context, p erp.Principal, company string) error {
	if p.Kind != erp.PrincipalUser || p.ID == "" || p.Credential == nil || s.ids == nil {
		return erp.ErrForbidden
	}
	id, err := s.ids.Whoami(ctx, p.Credential)
	if err != nil || id.User != p.ID || id.User == "Guest" {
		delete(s.live, userCompany{p.ID, company})
		if errors.Is(err, erp.ErrForbidden) || errors.Is(err, erp.ErrUnauthenticated) {
			if removeErr := sqlc.New(s.pool).RemoveAvatarPreference(ctx, sqlc.RemoveAvatarPreferenceParams{UserID: p.ID, Company: company}); removeErr != nil {
				return erp.ErrUnavailable
			}
		}
		return erp.ErrForbidden
	}
	if !slices.Contains(id.Companies, company) {
		delete(s.live, userCompany{p.ID, company})
		if s.pool != nil {
			if err := sqlc.New(s.pool).RemoveAvatarPreference(ctx, sqlc.RemoveAvatarPreferenceParams{UserID: p.ID, Company: company}); err != nil {
				return erp.ErrUnavailable
			}
		}
		return erp.ErrForbidden
	}
	return nil
}

func (s *Service) preference(ctx context.Context, p erp.Principal, company string) (Preference, error) {
	row, err := sqlc.New(s.pool).AvatarPreference(ctx, sqlc.AvatarPreferenceParams{UserID: p.ID, Company: company})
	if errors.Is(err, pgx.ErrNoRows) {
		return Preference{Appearance: "human-blue"}, nil
	}
	if err != nil {
		return Preference{}, erp.ErrUnavailable
	}
	return Preference{Consent: row.Consent, Appearance: row.Appearance}, nil
}

func (s *Service) Preference(ctx context.Context, p erp.Principal, company string) (Preference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.authority(ctx, p, company); err != nil {
		return Preference{}, err
	}
	return s.preference(ctx, p, company)
}

func (s *Service) SetPreference(ctx context.Context, p erp.Principal, company string, in Preference) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.authority(ctx, p, company); err != nil {
		return err
	}
	if !in.Consent && in.Appearance == "" {
		in.Appearance = "human-blue"
	}
	if !slices.Contains([]string{"human-blue", "human-coral", "human-green"}, in.Appearance) {
		return erp.ErrValidation
	}
	if err := sqlc.New(s.pool).SetAvatarPreference(ctx, sqlc.SetAvatarPreferenceParams{UserID: p.ID, Company: company, Consent: in.Consent, Appearance: in.Appearance}); err != nil {
		return erp.ErrUnavailable
	}
	key := userCompany{p.ID, company}
	if !in.Consent {
		delete(s.live, key)
	} else if e, ok := s.live[key]; ok {
		e.Appearance = in.Appearance
		s.live[key] = e
	}
	return nil
}

func (s *Service) Update(ctx context.Context, p erp.Principal, company, department, emote string) (Presence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.authority(ctx, p, company); err != nil {
		return Presence{}, err
	}
	if !slices.Contains([]string{"shop", "warehouse", "bank", "hq", "it", "post"}, department) || !slices.Contains([]string{"idle", "wave", "hello", "read", "type", "carry"}, emote) {
		return Presence{}, erp.ErrValidation
	}
	pref, err := s.preference(ctx, p, company)
	if err != nil {
		return Presence{}, err
	}
	if !pref.Consent {
		return Presence{}, erp.ErrForbidden
	}
	s.expire()
	key := userCompany{p.ID, company}
	e, ok := s.live[key]
	if !ok {
		if len(s.live) >= 200 {
			return Presence{}, erp.ErrUnavailable
		}
		e.ID = ulid.Make().String()
	}
	e.Department = department
	e.Emote = emote
	e.Appearance = pref.Appearance
	e.principal = p
	e.company = company
	e.expires = s.now().Add(time.Minute)
	s.live[key] = e
	return e.Presence, nil
}

func (s *Service) expire() {
	for key, e := range s.live {
		if !s.now().Before(e.expires) {
			delete(s.live, key)
		}
	}
}

func (s *Service) List(ctx context.Context, p erp.Principal, company string) ([]Presence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.authority(ctx, p, company); err != nil {
		return nil, err
	}
	s.expire()
	out := []Presence{}
	for key, e := range s.live {
		if e.company != company {
			continue
		}
		if s.authority(ctx, e.principal, company) != nil {
			delete(s.live, key)
			continue
		}
		out = append(out, e.Presence)
	}
	slices.SortFunc(out, func(a, b Presence) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out, nil
}

// Run sweeps idle clients too; no presence survives expiry or a failed account
// check merely because nobody polls. Shutdown discards credentials from memory.
func (s *Service) Run(ctx context.Context) error {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	defer func() { s.mu.Lock(); clear(s.live); s.mu.Unlock() }()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			cleanup, cancel := context.WithTimeout(ctx, 5*time.Second)
			s.mu.Lock()
			s.expire()
			for key, e := range s.live {
				if s.authority(cleanup, e.principal, e.company) != nil {
					delete(s.live, key)
				}
			}
			s.mu.Unlock()
			cancel()
		}
	}
}
