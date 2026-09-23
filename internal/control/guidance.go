package control

import (
	"fmt"
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
)

// SteerDisplay validates attachments under the current turn's cancellation scope.
// A late result must never be delivered to a successor turn.
func (c *Controller) SteerDisplay(input, display, expectedTurn, id string) error {
	c.mu.Lock()
	exec, ctx, turn := c.executor, c.activeContext, c.activeTurnID
	valid := c.running && !c.cancelRequested && exec != nil && ctx != nil && (expectedTurn == "" || expectedTurn == turn)
	c.mu.Unlock()
	if !valid {
		return fmt.Errorf("the task is no longer accepting guidance; your draft is preserved")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, ref := range parseRefTokens(input) {
		if isAttachmentRef(ref) && !fileRefExists(ref, c.cpRoot) {
			return fmt.Errorf("guidance attachment @%s is unavailable; your draft is preserved", ref)
		}
	}
	block, available, errs, err := c.resolveRefsWithImages(ctx, input, true)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		return fmt.Errorf("guidance attachments: %s", strings.Join(errs, "; "))
	}
	input, display, block, available, err = c.snapshotGuidanceImages(input, display, block, available)
	if err != nil {
		return err
	}
	images := available
	if !c.visionEnabled {
		images = nil
		block = strings.ReplaceAll(block, "[image content attached to this user message]", "[image snapshot available to the task tool; the current model did not receive image bytes]")
	}
	text := input
	if block != "" {
		text = "Referenced context:\n\n" + block + "\n\n" + input
	}
	c.mu.Lock()
	if !c.running || c.cancelRequested || c.activeTurnID != turn || c.executor != exec || ctx.Err() != nil {
		c.mu.Unlock()
		return fmt.Errorf("the task ended while preparing guidance; your draft is preserved")
	}
	ack, accepted := exec.TrySteerRich(agent.RichInput{Text: text, Images: images}, display, available, id)
	c.mu.Unlock()
	if !accepted {
		return fmt.Errorf("the task is no longer accepting guidance; your draft is preserved")
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		// Consumption may have won the race with cancellation.
		select {
		case err := <-ack:
			return err
		default:
			return ctx.Err()
		}
	}
}
