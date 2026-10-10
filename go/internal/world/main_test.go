package world

import (
	"testing"

	"go.uber.org/goleak"

	"github.com/atipongsena/erp-town/go/internal/store/storetest"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m, storetest.IgnoreBackgroundGoroutines()...) }
