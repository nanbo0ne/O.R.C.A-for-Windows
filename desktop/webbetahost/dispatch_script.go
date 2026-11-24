package webbetahost

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

//go:embed dispatch.js
var dispatchTemplate string

func canReuseDocument(state webbeta.BrowserState, generation, target string) bool {
	return webbeta.ValidChatURL(target) && state.URL == target && generation != "" && state.Ready && !state.Busy && state.Blocked == ""
}

// The wrapper reports execution, not just successful native queue insertion.
func dispatchScript(command []byte, generation string) string {
	gen, _ := json.Marshal(generation)
	return strings.NewReplacer("__ORCA_COMMAND__", string(command), "__ORCA_GENERATION__", string(gen)).Replace(dispatchTemplate)
}
