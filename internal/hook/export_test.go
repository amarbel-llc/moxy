package hook

import "sync"

// resetResolverForTest clears the package-level resolver cache so tests
// that swap MOXIN_PATH between cases see a fresh discovery on the next
// hook invocation. Lives in *_test.go so the production binary doesn't
// ship a reset path that could race with concurrent hook calls.
func resetResolverForTest() {
	permResolver = nil
	permResolverErr = nil
	permResolverOnce = sync.Once{}
}
