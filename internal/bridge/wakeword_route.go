package bridge

import (
	"os"
	"sync"
	"time"

	"quasarbridge/internal/dialogue"
)

// Voice activations have strict destinations. In particular, no learned
// smart-home rule or classifier may intercept an unmarked Alice activation.
func explicitWakewordDecision(marker, text string) RouteDecision {
	switch marker {
	case "llm":
		if dialogue.IsReset(text) {
			return RouteDecision{Route: "llm", Source: "dialogue_reset"}
		}
		return RouteDecision{Route: "llm", Source: "wakeword"}
	case "home":
		return RouteDecision{Route: "home", Source: "wakeword"}
	default:
		return RouteDecision{Route: "yandex", Source: "wakeword"}
	}
}

// wakewordRouteMaxAge bounds how long a marker stays valid after it is written.
const wakewordRouteMaxAge = 3 * time.Second

var wakewordRouteMu sync.Mutex

// claimWakewordRoute reports whether a fresh marker file asks for the next
// voice request to be answered by the LLM. The marker is written by a local
// wake-word daemon (for example station-wakeword's "llm" action) just before it
// starts a conversation. It is consumed at most once, so an abandoned or stale
// activation never reroutes a later ordinary Alice request.
func claimWakewordRoute(path string, now time.Time) string {
	if path == "" {
		return ""
	}
	wakewordRouteMu.Lock()
	defer wakewordRouteMu.Unlock()
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	defer os.Remove(path)
	age := now.Sub(info.ModTime())
	if !info.Mode().IsRegular() || age < 0 || age > wakewordRouteMaxAge {
		return ""
	}
	b, err := os.ReadFile(path)
	if err == nil && (string(b) == "llm\n" || string(b) == "home\n") {
		return string(b[:len(b)-1])
	}
	return ""
}
