// Package orcaaccount owns desktop account authentication, not conversation history.
// Only secret-free DTOs should be bound to the desktop UI.
package orcaaccount

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	Issuer    = "https://sub.aichat.diy"
	Router    = "https://router.aichat.diy"
	ClientID  = "orca-beta"
	APIPrefix = "/api/v1/desktop"
)

var (
	ErrUnauthenticated    = errors.New("orca account: sign-in required")
	ErrStale              = errors.New("orca account: account generation changed")
	ErrProtocol           = errors.New("orca account: invalid server response")
	ErrNetwork            = errors.New("orca account: request unavailable")
	ErrServiceUnavailable = errors.New("orca account: desktop service not deployed")
	ErrAuthorization      = errors.New("orca account: authorization rejected")
	ErrStorage            = errors.New("orca account: secure storage failed")
	ErrUnsupported        = errors.New("orca account: secure storage unsupported")
	ErrNotFound           = errors.New("orca account: no stored session")
	ErrInvalid            = errors.New("orca account: invalid argument")
	ErrBrowser            = errors.New("orca account: system browser unavailable")
	ErrClosed             = errors.New("orca account: client closed")
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type Profile struct {
	Subject     string    `json:"subject"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email,omitempty"`
	Groups      []Group   `json:"groups"`
	User        User      `json:"user"`
	SessionID   string    `json:"session_id"`
	DeviceID    string    `json:"device_id"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Group struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Snapshot struct {
	Generation    uint64  `json:"generation"`
	Closed        bool    `json:"closed"`
	Authenticated bool    `json:"authenticated"`
	Profile       Profile `json:"profile"`
}

type Model struct {
	ID             string                     `json:"id"`
	Name           string                     `json:"name"`
	PublicationID  string                     `json:"publication_id"`
	GroupID        int64                      `json:"group_id"`
	GroupName      string                     `json:"group_name"`
	Protocol       string                     `json:"protocol"`
	Endpoint       string                     `json:"endpoint"`
	Capabilities   *ModelCapabilities         `json:"capabilities"`
	Prices         *Prices                    `json:"prices"`
	PricingVersion map[string]json.RawMessage `json:"pricing_version"`
}

// Nil is explicitly unknown, not false/zero. Never infer capabilities from ID.
type ModelCapabilities struct {
	Text              *bool    `json:"text"`
	Streaming         *bool    `json:"streaming"`
	Tools             *bool    `json:"tools"`
	Reasoning         *bool    `json:"reasoning"`
	Vision            *bool    `json:"vision"`
	ContextWindow     *int64   `json:"context_window"`
	BudgetMethod      string   `json:"budget_method"`
	ReasoningStatus   string   `json:"reasoning_status,omitempty"`
	ReasoningEfforts  []string `json:"reasoning_efforts,omitempty"`
	DefaultEffort     string   `json:"default_effort,omitempty"`
	ReasoningSource   string   `json:"reasoning_source,omitempty"`
	ReasoningRevision int64    `json:"reasoning_revision,omitempty"`
}

// Prices preserves the server's exact integer/fractional price representation.
// These are price-table units, not per-request settled charges.
type Prices struct {
	Request          *RequestPrice     `json:"_request,omitempty"`
	Input            *int64            `json:"input,omitempty"`
	Output           *int64            `json:"output,omitempty"`
	Cache            *int64            `json:"cache,omitempty"`
	Write5m          *int64            `json:"cacheWrite5m,omitempty"`
	Write1h          *int64            `json:"cacheWrite1h,omitempty"`
	Write30m         *int64            `json:"cacheWrite30m,omitempty"`
	Write12h         *int64            `json:"cacheWrite12h,omitempty"`
	Fractional       map[string]string `json:"_fractional,omitempty"`
	Long             *LongPrices       `json:"_long,omitempty"`
	CacheWrites      bool              `json:"_cacheWrites,omitempty"`
	MaxVerifiedInput int64             `json:"_maxVerifiedInput,omitempty"`
}

// A server price quote is not proof that a particular request was settled.
type RequestPrice struct {
	Micros          int64 `json:"micros"`
	EmptyOutputFree bool  `json:"emptyOutputFree"`
}

type LongPrices struct {
	Threshold int64  `json:"threshold"`
	Prices    Prices `json:"prices"`
}

type Usage struct {
	Input         *int64 `json:"input"`
	UncachedInput *int64 `json:"uncachedInput"`
	Output        *int64 `json:"output"`
	CacheRead     *int64 `json:"cacheRead"`
	Write5m       *int64 `json:"cacheWrite5m"`
	Write1h       *int64 `json:"cacheWrite1h"`
	Write30m      *int64 `json:"cacheWrite30m"`
	Write12h      *int64 `json:"cacheWrite12h"`
	Semantic      string `json:"semantic,omitempty"`
	Status        string `json:"status,omitempty"`
	ObservedModel string `json:"observed_model,omitempty"`
	ModelConflict bool   `json:"model_conflict,omitempty"`
}

type Models struct {
	Models   []Model `json:"models"`
	Currency string  `json:"currency"`
}

// Money is always signed int64 CNY micros. Gifts never contribute to CashMicros.
type Wallet struct {
	Currency        string       `json:"currency"`
	CashMicros      int64        `json:"cash_micros"`
	HeldMicros      int64        `json:"held_micros"`
	AvailableMicros int64        `json:"available_micros"`
	ScopedGifts     []ScopedGift `json:"scoped_gifts"`
}

type ScopedGift struct {
	ID              string     `json:"id"`
	GroupID         int64      `json:"group_id"`
	GroupIDs        []int64    `json:"group_ids"`
	BalanceMicros   int64      `json:"balance_micros"`
	HeldMicros      int64      `json:"held_micros"`
	AvailableMicros int64      `json:"available_micros"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

type SettlementStatus string

const (
	SettlementEstimated SettlementStatus = "estimated"
	SettlementPending   SettlementStatus = "pending"
	SettlementSettled   SettlementStatus = "settled"
	SettlementUnknown   SettlementStatus = "unknown"
)

type Settlement struct {
	RequestID       string           `json:"request_id"`
	Status          SettlementStatus `json:"status"`
	Currency        string           `json:"currency"`
	EstimatedMicros *int64           `json:"estimated_micros,omitempty"`
	SettledMicros   *int64           `json:"settled_micros,omitempty"`
	CashCostMicros  *int64           `json:"cash_cost_micros,omitempty"`
	GiftCostMicros  *int64           `json:"gift_cost_micros,omitempty"`
	Usage           *Usage           `json:"usage,omitempty"`
}

type CredentialOptions struct {
	GroupID *int64 `json:"group_id,omitempty"`
}

type CredentialInfo struct {
	CredentialID string    `json:"credential_id"`
	BaseURL      string    `json:"base_url"`
	GroupID      *int64    `json:"group_id,omitempty"`
	Restricted   bool      `json:"restricted"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Secret-bearing wire types are private, never returned by UI-facing methods.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	SessionID        string `json:"session_id"`
	DeviceID         string `json:"device_id"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
	Scope            string `json:"scope"`
	Subject          string `json:"subject"`
	Issuer           string `json:"issuer"`
	Nonce            string `json:"nonce"`
}

type credentialResponse struct {
	APIKey       string    `json:"api_key"`
	CredentialID string    `json:"credential_id"`
	BaseURL      string    `json:"base_url"`
	Restricted   bool      `json:"restricted"`
	GroupID      *int64    `json:"group_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (tokenResponse) String() string          { return "[redacted token response]" }
func (v tokenResponse) GoString() string      { return v.String() }
func (credentialResponse) String() string     { return "[redacted credential response]" }
func (v credentialResponse) GoString() string { return v.String() }
