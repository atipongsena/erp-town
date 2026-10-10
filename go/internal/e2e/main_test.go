//go:build !integration

package e2e

import (
	"testing"

	"go.uber.org/goleak"
)

// Integration runs use the existing TestMain in sales_flow_test.go.
func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
