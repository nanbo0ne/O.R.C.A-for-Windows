package control

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/webbeta"
)

func (c *Controller) resolveWebBetaInput(ctx context.Context, input, refLine string) (agent.RichInput, error) {
	rich := agent.RichInput{Text: input}
	if err := ctx.Err(); err != nil {
		return rich, err
	}
	for _, ref := range parseRefTokens(refLine) {
		if isAttachmentRef(ref) && !fileRefExists(ref, c.cpRoot) {
			return rich, fmt.Errorf("attachment @%s is unavailable; reattach before sending", ref)
		}
	}
	refs := resolveBareNames(c.detectRefs(refLine), c.cpRoot)
	if len(refs) == 0 {
		return rich, nil
	}
	root, err := openTaskImageRoot(c.cpRoot)
	if err != nil {
		return rich, err
	}
	defer root.Close()
	gate := taskImagePolicyGate{Gate: c.newInteractiveGate(), policy: c.policy, workspace: c.cpRoot}
	vault := webbeta.NewVault()
	defer vault.Invalidate()
	seen := map[string]bool{}
	var ids []string
	for _, r := range refs {
		if err := ctx.Err(); err != nil {
			return rich, err
		}
		if r.kind != refFile && r.kind != refImage {
			return rich, fmt.Errorf("web Beta only accepts explicit file/image attachments")
		}
		if r.kind == refImage && c.visionMode == "off" {
			return rich, fmt.Errorf("vision is disabled; enable it before uploading this image")
		}
		rel, err := taskImageRelativePath(c.cpRoot, r.path)
		if err != nil {
			return rich, err
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if err := checkTaskImagePermission(ctx, gate, "read_file", map[string]any{"path": filepath.Join(c.cpRoot, rel), "purpose": "web Beta attachment snapshot"}); err != nil {
			return rich, err
		}
		// The established snapshot reader pins directories and checks handles,
		// links, size, identity and TOCTOU without re-opening by an absolute path.
		raw, err := readTaskImage(root, rel)
		if err != nil {
			return rich, err
		}
		id, err := vault.Grant("turn", "send", filepath.Base(r.path), raw)
		if err != nil {
			return rich, err
		}
		ids = append(ids, id)
		rich.Text = strings.ReplaceAll(rich.Text, "@"+r.raw, "[附件："+filepath.Base(r.path)+"]")
	}
	uploads, err := vault.Resolve("turn", "send", ids)
	if err != nil {
		return rich, err
	}
	manifest := make([]map[string]any, 0, len(uploads))
	for _, u := range uploads {
		manifest = append(manifest, map[string]any{"name": u.Name, "bytes": len(u.Data), "sha256": fmt.Sprintf("%x", sha256.Sum256(u.Data))})
	}
	if err := checkTaskImagePermission(ctx, gate, "file_send", map[string]any{"path": c.modelRef, "destination_model": c.modelRef, "files": manifest}); err != nil {
		return rich, err
	}
	for _, u := range uploads {
		rich.Files = append(rich.Files, provider.FileContent{Name: u.Name, MediaType: u.MIME, Digest: fmt.Sprintf("%x", sha256.Sum256(u.Data)), Data: u.Data})
	}
	return rich, ctx.Err()
}
