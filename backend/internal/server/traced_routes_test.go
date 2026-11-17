package server

import (
	"maps"
	"strings"
	"testing"
)

// A ROUTE NAMED AS ALWAYS OR NEVER TRACED THAT IS NOT REGISTERED STOPS THE
// SERVER AT CONSTRUCTION (#768): renamed without its line in alwaysTraced, it
// would otherwise fall silently to one in ten.
func TestATracedRouteThatIsNotRegisteredPanicsAtConstruction(t *testing.T) {
	registered := map[string]bool{}
	for pattern := range maps.Keys(alwaysTraced) {
		registered[pattern] = true
	}
	for pattern := range maps.Keys(neverTraced) {
		registered[pattern] = true
	}
	requireTracedRoutesRegistered(registered)

	const renamed = "POST /api/v1/public/checkout/{clientTransactionId}/confirm"
	delete(registered, renamed)
	defer func() {
		message, _ := recover().(string)
		if !strings.Contains(message, renamed) {
			t.Errorf("panic = %q, want one naming %q", message, renamed)
		}
	}()
	requireTracedRoutesRegistered(registered)
}
