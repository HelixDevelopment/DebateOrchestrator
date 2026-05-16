package comprehensive

import (
	"errors"
	"sync"
)

var (
	invokerMu       sync.RWMutex
	invokerRegistry = map[string]LLMInvoker{}
	fallbackInvoker LLMInvoker
)

// RegisterInvoker records an LLMInvoker under the (provider, model) key.
// Re-registration overwrites the prior entry.
func RegisterInvoker(provider, model string, invoker LLMInvoker) error {
	if provider == "" {
		return errors.New("debate/comprehensive: provider required")
	}
	if model == "" {
		return errors.New("debate/comprehensive: model required")
	}
	if invoker == nil {
		return errors.New("debate/comprehensive: invoker required")
	}
	invokerMu.Lock()
	defer invokerMu.Unlock()
	invokerRegistry[provider+"/"+model] = invoker
	return nil
}

// SetFallbackInvoker installs an invoker used when no per-(provider, model)
// match is found.
func SetFallbackInvoker(invoker LLMInvoker) {
	invokerMu.Lock()
	defer invokerMu.Unlock()
	fallbackInvoker = invoker
}

// LookupInvoker returns the most-specific invoker registered for the
// supplied (provider, model) pair. It is exported so tests can verify
// registration without poking at unexported state.
func LookupInvoker(provider, model string) LLMInvoker {
	invokerMu.RLock()
	defer invokerMu.RUnlock()
	if inv, ok := invokerRegistry[provider+"/"+model]; ok {
		return inv
	}
	return fallbackInvoker
}

// HasFallbackInvoker reports whether a fallback invoker is currently
// installed. Useful for callers that want to decide between dispatching
// to comprehensive and using a direct provider path.
func HasFallbackInvoker() bool {
	invokerMu.RLock()
	defer invokerMu.RUnlock()
	return fallbackInvoker != nil
}
