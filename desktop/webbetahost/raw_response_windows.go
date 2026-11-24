//go:build windows

package webbetahost

import (
	_ "embed"
	"encoding/json"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

//go:embed raw-response.js
var rawResponseScript string

// Internal page DTO: Operation deliberately remains absent from the core wire type.
func marshalPageCommand(cmd webbeta.Command) ([]byte, error) {
	return json.Marshal(struct {
		webbeta.Command
		Opening bool `json:"opening"`
	}{cmd, cmd.Operation == "opening"})
}
