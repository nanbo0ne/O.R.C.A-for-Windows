//go:build windows

package browserhost

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

type actionFenceKey struct{}
type actionFence struct {
	document   string
	generation uint64
}

func (h *Host) approvedHit(ctx context.Context, id, ref string, x, y float64) bool {
	world, err := h.isolatedWorld(ctx, id)
	if err != nil {
		return false
	}
	data, err := h.cdp(ctx, id, "DOM.getNodeForLocation", map[string]any{"x": int(math.Round(x)), "y": int(math.Round(y)), "includeUserAgentShadowDOM": false, "ignorePointerEventsNone": false})
	var hit struct {
		BackendNodeID int64 `json:"backendNodeId"`
	}
	if err != nil || json.Unmarshal(data, &hit) != nil || hit.BackendNodeID == 0 {
		return false
	}
	data, err = h.cdp(ctx, id, "DOM.resolveNode", map[string]any{"backendNodeId": hit.BackendNodeID, "executionContextId": world})
	var resolved struct {
		Object struct {
			ID string `json:"objectId"`
		} `json:"object"`
	}
	if err != nil || json.Unmarshal(data, &resolved) != nil || resolved.Object.ID == "" {
		return false
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = h.cdp(cleanup, id, "Runtime.releaseObject", map[string]any{"objectId": resolved.Object.ID})
	}()
	data, err = h.cdp(ctx, id, "Runtime.callFunctionOn", map[string]any{
		"objectId": resolved.Object.ID, "returnByValue": true, "arguments": []map[string]any{{"value": ref}},
		"functionDeclaration": `function(ref){const hit=this.nodeType===3?this.parentElement:this;const target=ref?window.__orcaObserved?.get(ref):hit;if(!hit||!target||!target.isConnected||target.disabled)return false;if(hit.ownerDocument!==document||target.ownerDocument!==document)return false;if(hit.closest('iframe,frame,object,embed'))return false;const root=hit.getRootNode();const sameTree=hit===target||target.contains(hit);const ownShadow=root&&root.host&&(root.host===target||target.contains(root.host));if(!sameTree&&!ownShadow)return false;if(!ref&&hit.tagName!=='CANVAS')return false;const sensitive=e=>e&&(e.type==='password'||e.type==='file'||/cc-|one-time-code/i.test(e.autocomplete||''));return ![hit.closest('input,textarea,[contenteditable]'),target.closest('input,textarea,[contenteditable]'),hit.closest('label')?.control,target.closest('label')?.control].some(sensitive);}`,
	})
	var result struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
	}
	return err == nil && json.Unmarshal(data, &result) == nil && result.Result.Value
}

// Closed shadow roots are invisible to page JavaScript, including our isolated
// world. Inspect Chromium's DOM tree before exposing rendered pixels.
func (h *Host) screenshotBoundary(ctx context.Context, id string) string {
	data, err := h.cdp(ctx, id, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true})
	if err != nil || len(data) > 2*1024*1024 {
		return "Screenshot document boundary could not be verified"
	}
	type node struct {
		Name            string            `json:"nodeName"`
		Children        []json.RawMessage `json:"children"`
		ShadowRoots     []json.RawMessage `json:"shadowRoots"`
		ContentDocument json.RawMessage   `json:"contentDocument"`
	}
	var root struct {
		Root json.RawMessage `json:"root"`
	}
	if json.Unmarshal(data, &root) != nil || len(root.Root) == 0 {
		return "Screenshot document boundary could not be verified"
	}
	queue := []json.RawMessage{root.Root}
	for count := 0; len(queue) > 0; count++ {
		if count > 30000 {
			return "Screenshot document is too complex to verify"
		}
		var n node
		if json.Unmarshal(queue[0], &n) != nil {
			return "Screenshot document boundary could not be verified"
		}
		queue = queue[1:]
		switch strings.ToUpper(n.Name) {
		case "IFRAME", "FRAME", "OBJECT", "EMBED":
			return "Embedded content requires manual inspection"
		}
		if len(n.ContentDocument) > 0 && string(n.ContentDocument) != "null" {
			return "Embedded or shadow content requires manual inspection"
		}
		for _, raw := range n.ShadowRoots {
			var shadow struct {
				Type string `json:"shadowRootType"`
			}
			if json.Unmarshal(raw, &shadow) != nil || shadow.Type != "user-agent" {
				return "Embedded or shadow content requires manual inspection"
			}
		}
		queue = append(queue, n.Children...)
	}
	return ""
}

// References live in a separate JavaScript world; the page cannot replace them
// or the helper builtins. DOM mutation invalidates an observation before input.
func (h *Host) isolatedWorld(ctx context.Context, id string) (int64, error) {
	var world int64
	var document string
	if err := h.call(ctx, func() error {
		t, err := h.tab(id)
		if err == nil {
			world, document = t.world, t.document
		}
		return err
	}); err != nil {
		return 0, err
	}
	if world != 0 {
		return world, nil
	}
	data, err := h.cdp(ctx, id, "Page.getFrameTree", map[string]any{})
	if err != nil {
		return 0, err
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err = json.Unmarshal(data, &tree); err != nil {
		return 0, err
	}
	if tree.FrameTree.Frame.ID == "" {
		return 0, errors.New("browser document not ready")
	}
	data, err = h.cdp(ctx, id, "Page.createIsolatedWorld", map[string]any{"frameId": tree.FrameTree.Frame.ID, "worldName": "orca-browser-observation", "grantUniveralAccess": false})
	if err != nil {
		return 0, err
	}
	var result struct {
		ID int64 `json:"executionContextId"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return 0, err
	}
	if result.ID == 0 {
		return 0, errors.New("browser observation world unavailable")
	}
	err = h.call(ctx, func() error {
		t, e := h.tab(id)
		if e != nil {
			return e
		}
		if document != t.document {
			return ErrStaleObservation
		}
		t.world = result.ID
		return nil
	})
	return result.ID, err
}
