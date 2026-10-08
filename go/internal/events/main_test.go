package events

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package if a test leaves goroutines behind. Only the goroutines
// testcontainers keeps for the whole binary (Ryuk reaper connection, Docker API
// keep-alive connections, the Windows named-pipe poller) are ignored; pgx pools and
// redis clients must be closed by the tests.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
		goleak.IgnoreAnyFunction("github.com/testcontainers/testcontainers-go.(*Reaper).Connect.func1"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreAnyFunction("github.com/Microsoft/go-winio.ioCompletionProcessor"),
	)
}
