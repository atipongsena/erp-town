package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/atipongsena/erp-town/go/internal/platform"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func waitDone(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func TestRunAllFirstErrorCancelsTheRest(t *testing.T) {
	boom := errors.New("boom")
	err := runAll(t.Context(), waitDone, func(context.Context) error { return boom }, waitDone)
	require.ErrorIs(t, err, boom)
}

func TestRunAllCleanShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, runAll(ctx, waitDone, waitDone))
}

func TestServeRejectsBadJWTKey(t *testing.T) {
	cfg := platform.Config{HTTPAddr: "127.0.0.1:0", JWTPrivateKey: "bad", Roles: platform.Roles{platform.RoleGateway: true}}
	err := serve(t.Context(), cfg)
	assert.ErrorContains(t, err, "jwt seed")
}
