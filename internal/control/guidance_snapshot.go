package control

import (
	"encoding/base64"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Freeze already-validated bytes under fresh attachment names, so later file
// edits cannot replace guidance images when its session is reopened.
func (c *Controller) snapshotGuidanceImages(input, display, block string, images []provider.ImageContent) (string, string, string, []provider.ImageContent, error) {
	images = append([]provider.ImageContent(nil), images...)
	for i := range images {
		raw, err := base64.StdEncoding.DecodeString(images[i].Data)
		if err != nil {
			return input, display, block, nil, err
		}
		path, err := SaveImageBytesAt(c.cpRoot, images[i].MediaType, raw)
		if err != nil {
			return input, display, block, nil, err
		}
		old := images[i].Path
		input = strings.ReplaceAll(input, old, path)
		display = strings.ReplaceAll(display, old, path)
		block = strings.ReplaceAll(block, old, path)
		images[i].Path = path
	}
	return input, display, block, images, nil
}
