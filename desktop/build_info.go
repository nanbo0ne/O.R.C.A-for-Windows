package main

import (
	"encoding/json"
	"errors"
	"io"
)

const buildInfoJSONArg = "--orca-build-info-json"

// This private CLI probe must run before any normal main processing. It reads
// only linker-injected strings; it must never load configuration or start Wails.
func writeBuildInfoJSON(args []string, out io.Writer) (bool, error) {
	for _, arg := range args {
		if arg != buildInfoJSONArg {
			continue
		}
		if len(args) != 1 {
			return true, errors.New("--orca-build-info-json must be the only argument")
		}
		return true, json.NewEncoder(out).Encode(struct {
			SchemaVersion int    `json:"schemaVersion"`
			Channel       string `json:"channel"`
			Version       string `json:"version"`
		}{1, channel, version})
	}
	return false, nil
}
