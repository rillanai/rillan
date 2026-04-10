// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const (
	EventTypeRemoteEgress  = "remote_egress"
	EventTypeRemoteDeny    = "remote_deny"
	EventTypeAgentProposal = "agent_action_proposed"
	EventTypeAgentApproved = "agent_action_approved"
	EventTypeAgentDenied   = "agent_action_denied"
)

type Event struct {
	Timestamp      time.Time `json:"timestamp"`
	Type           string    `json:"type"`
	RequestID      string    `json:"request_id"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	Verdict        string    `json:"verdict"`
	Reason         string    `json:"reason"`
	RouteSource    string    `json:"route_source"`
	OutboundSHA256 string    `json:"outbound_sha256,omitempty"`
	SourceRefs     []string  `json:"source_refs,omitempty"`
	ResponseStatus int       `json:"response_status,omitempty"`
	ResponseSHA256 string    `json:"response_sha256,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type Recorder interface {
	Record(ctx context.Context, event Event) error
}

func HashBytes(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
