package webbeta

import (
	"context"
	"errors"
	"sort"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

var ErrIdentityClosed = errors.New("web beta identity closed; start a new conversation after login")
var ErrIdentityChanged = errors.New("web beta conversation belongs to a previous identity; start a new conversation")

const identityResetPending = "identity_reset_pending"
const identityDrainPending = "identity_drain_pending"
const IdentitySwitchPending = "identity_switch_pending"

// This user-initiated transition keeps the site login and the source grant,
// but never carries a conversation or a running tool into the new identity.
func (e *Engine) StopForAccountSwitch() error {
	e.mu.Lock()
	if e.state.Blocked == identityDrainPending || e.state.Blocked == identityResetPending {
		e.mu.Unlock()
		return ErrIdentityClosed
	}
	e.state.Settings.Enabled = false
	e.state.Settings.ReservePaused = true
	e.state.Blocked = IdentitySwitchPending
	e.state.InvalidOwners = e.identityOwnersLocked()
	err := saveState(e.path, e.state)
	e.mu.Unlock()
	e.startClose()
	return err
}

// PrepareAccountSwitch requires observed drain, not merely a cancel receipt.
// Archived bindings remain local evidence; no new owner can acquire them.
func (e *Engine) PrepareAccountSwitch() error {
	select {
	case <-e.closeDone:
	default:
		return ErrTaskDraining
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state.Blocked != IdentitySwitchPending {
		return ErrIdentityClosed
	}
	next := e.state
	next.Generation = newID()
	next.InvalidOwners = e.identityOwnersLocked()
	next.Conversations = append([]Conversation(nil), e.state.Conversations...)
	for i := range next.Conversations {
		c := &next.Conversations[i]
		c.Phase, c.Inflight, c.Recovery = "archived", "", nil
	}
	if err := saveState(e.path, next); err != nil {
		return err
	}
	e.state = next
	return nil
}

// Called on a fresh Engine/Browser after an explicit account switch and a
// successful read-only site check. Startup alone must never finish this step.
func (e *Engine) ConfirmAccountSwitch() error {
	state := e.browser.State()
	if !state.Ready || state.Busy || state.Blocked != "" {
		return errors.New("web_recheck_not_ready")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.state.Blocked != IdentitySwitchPending {
		return ErrIdentityClosed
	}
	if e.activeOperations != 0 || len(e.requestCancels) != 0 || len(e.streams) != 0 || e.filling || e.stoppingLocked() {
		return ErrTaskDraining
	}
	for _, c := range e.state.Conversations {
		if c.Phase != "archived" || c.Inflight != "" {
			return ErrIdentityClosed
		}
	}
	next := e.state
	next.Blocked = ""
	next.Settings.Enabled = next.SendAuthorized
	next.Settings.ReservePaused = true
	if err := saveState(e.path, next); err != nil {
		return err
	}
	e.state, e.lastError = next, ""
	if next.Settings.Enabled {
		e.queue.Resume()
	}
	return nil
}

// A page completion is not an acknowledgement until its checkpoint is durable.
// Hold mu and the scheduler slot through failure latching, even if storage is
// still unavailable and the best-effort uncertainty journal also cannot write.
func (e *Engine) saveCompletionLocked(c *Conversation) error {
	if err := saveState(e.path, e.state); err != nil {
		e.uncertainLocked(c)
		return provider.NoRequestReplay(err)
	}
	return nil
}

// Called while mu and the scheduler slot are held: no other owner may send
// between an ambiguous completion and the account-wide pause.
func (e *Engine) uncertainLocked(c *Conversation) {
	if c != nil {
		c.Phase = "uncertain"
		c.Recovery = nil
	}
	if e.state.Blocked == "" {
		e.state.Blocked = "send_unconfirmed"
	}
	e.state.Settings.Enabled = false
	e.state.Settings.ReservePaused = true
	e.queue.Pause()
	e.queue.RejectPending(&Failure{Code: e.state.Blocked, Sent: true})
	_ = saveState(e.path, e.state)
}

func (e *Engine) beginOperation(owner string) (func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrIdentityClosed
	}
	if e.stoppingLocked() {
		return nil, ErrTaskDraining
	}
	for _, old := range e.state.InvalidOwners {
		if old == owner {
			return nil, ErrIdentityChanged
		}
	}
	if owner != "" {
		e.owners[owner] = true
	}
	// Add and the close barrier share mu: Wait cannot race with a new Add.
	e.operations.Add(1)
	e.activeOperations++
	return func() {
		e.mu.Lock()
		e.activeOperations--
		e.mu.Unlock()
		e.operations.Done()
	}, nil
}

type identityCancellation struct{ cancel context.CancelFunc }

func (e *Engine) requestContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	entry := &identityCancellation{cancel: cancel}
	e.mu.Lock()
	if e.closed {
		cancel()
	} else {
		if e.requestCancels == nil {
			e.requestCancels = make(map[*identityCancellation]struct{})
		}
		e.requestCancels[entry] = struct{}{}
	}
	e.mu.Unlock()
	return ctx, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		cancel()
		delete(e.requestCancels, entry)
	}
}

// Close waits for stop receipts, queued callers and adapter post-processing.
// It preserves saved consent for ordinary application restart, but old Engine
// and Vault pointers are permanently unusable.
func (e *Engine) Close() {
	_ = e.CloseContext(context.Background())
}

// CloseContext starts irreversible cancellation even if the caller times out.
// Completion still requires the scheduler and every adapter operation to exit.
func (e *Engine) CloseContext(ctx context.Context) error {
	e.startClose()
	select {
	case <-e.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) startClose() {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		e.cancel()
		// Revocation must be visible before returning to a permission gate or
		// commit path. AfterFunc callbacks alone leave a scheduling gap here.
		for entry := range e.requestCancels {
			entry.cancel()
		}
		clear(e.requestCancels)
		e.vault.Invalidate()
		e.mu.Unlock()
		go func() {
			e.queue.Close()
			e.operations.Wait()
			close(e.closeDone)
		}()
	})
}

// StopForIdentityChange persists the fail-closed boundary without deleting old
// bindings or changing generation. Only a proven drain may begin the reset.
func (e *Engine) StopForIdentityChange() error {
	e.mu.Lock()
	e.state.SendAuthorized = false
	e.state.Settings.Enabled = false
	e.state.Settings.ReservePaused = true
	e.state.Blocked = identityDrainPending
	e.state.InvalidOwners = e.identityOwnersLocked()
	err := saveState(e.path, e.state)
	e.mu.Unlock()
	e.startClose()
	return err
}

// BeginIdentityReset journals the boundary before profile deletion. On crash,
// reopening must finish the reset, not reopen the old logged-in profile.
func (e *Engine) BeginIdentityReset() error {
	e.Close()
	e.mu.Lock()
	defer e.mu.Unlock()
	owners := e.identityOwnersLocked()
	next := State{Version: stateVersion, Generation: newID(), Sent: e.state.Sent,
		Settings: DefaultSettings(), InvalidOwners: owners, Tasks: e.state.Tasks, Blocked: identityResetPending}
	if err := saveState(e.path, next); err != nil {
		return err
	}
	e.state = next
	return nil
}

func (e *Engine) identityOwnersLocked() []string {
	for _, owner := range e.state.InvalidOwners {
		e.owners[owner] = true
	}
	for _, c := range e.state.Conversations {
		if c.Owner != "" {
			e.owners[c.Owner] = true
		}
	}
	owners := make([]string, 0, len(e.owners))
	for owner := range e.owners {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	return owners
}

func IdentityResetPending(path string) (bool, error) {
	s, err := loadState(path)
	return s.Blocked == identityResetPending, err
}

// CompleteIdentityReset is called only after native release and owned-profile
// deletion succeeded. It never authorizes sends or restores conversation URLs.
func CompleteIdentityReset(path string) error {
	s, err := loadState(path)
	if err != nil {
		return err
	}
	if s.Blocked != identityResetPending {
		return errors.New("web beta identity reset was not journaled")
	}
	s.Blocked = ""
	s.SendAuthorized = false
	s.Settings.Enabled = false
	s.Settings.ReservePaused = true
	s.Conversations = nil
	return saveState(path, s)
}

// The caller has detached and drained any live Engine before using this.
func BeginStoredIdentityReset(path string) error {
	s, err := loadState(path)
	if err != nil {
		return err
	}
	owners := make(map[string]bool)
	for _, owner := range s.InvalidOwners {
		owners[owner] = true
	}
	for _, c := range s.Conversations {
		if c.Owner != "" {
			owners[c.Owner] = true
		}
	}
	var invalid []string
	for owner := range owners {
		invalid = append(invalid, owner)
	}
	sort.Strings(invalid)
	return saveState(path, State{Version: stateVersion, Generation: newID(), Sent: s.Sent,
		Settings: DefaultSettings(), InvalidOwners: invalid, Tasks: s.Tasks, Blocked: identityResetPending})
}
