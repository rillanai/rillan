// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"fmt"
	"sync"
)

type ProposalStore struct {
	mu        sync.Mutex
	proposals map[string]ActionProposal
}

func NewProposalStore() *ProposalStore {
	return &ProposalStore{proposals: make(map[string]ActionProposal)}
}

func (s *ProposalStore) Put(proposal ActionProposal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proposals[proposal.ID] = proposal
}

func (s *ProposalStore) Get(id string) (ActionProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proposal, ok := s.proposals[id]
	if !ok {
		return ActionProposal{}, ErrProposalNotFound
	}
	return proposal, nil
}

func (s *ProposalStore) UpdateStatus(id string, status string) (ActionProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proposal, ok := s.proposals[id]
	if !ok {
		return ActionProposal{}, ErrProposalNotFound
	}
	proposal.Status = status
	s.proposals[id] = proposal
	return proposal, nil
}

func (s *ProposalStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.proposals)
}

var ErrProposalNotFound = fmt.Errorf("proposal not found")
