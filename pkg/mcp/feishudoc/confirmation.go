package feishudoc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type pendingAction struct {
	ID          string
	Tool        string
	ContextKey  string
	SenderID    string
	PayloadHash string
	Preview     string
	ExpiresAt   time.Time
}

type confirmationManager struct {
	mu      sync.Mutex
	ttl     time.Duration
	actions map[string]*pendingAction
}

func newConfirmationManager(ttl time.Duration) *confirmationManager {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	return &confirmationManager{
		ttl:     ttl,
		actions: make(map[string]*pendingAction),
	}
}

func (m *confirmationManager) Create(
	tool string,
	contextKey string,
	senderID string,
	payload any,
	preview string,
	now time.Time,
) (*pendingAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.purgeExpiredLocked(now)

	payloadHash, err := payloadHash(payload)
	if err != nil {
		return nil, err
	}

	action := &pendingAction{
		ID:          uuid.NewString(),
		Tool:        tool,
		ContextKey:  contextKey,
		SenderID:    senderID,
		PayloadHash: payloadHash,
		Preview:     preview,
		ExpiresAt:   now.Add(m.ttl),
	}
	m.actions[action.ID] = action
	return action, nil
}

func (m *confirmationManager) Validate(
	actionID string,
	tool string,
	contextKey string,
	payload any,
	now time.Time,
) (*pendingAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.purgeExpiredLocked(now)

	action, ok := m.actions[actionID]
	if !ok {
		return nil, fmt.Errorf("action_id %q not found or expired", actionID)
	}

	if action.Tool != tool {
		return nil, fmt.Errorf("action_id %q does not match tool %q", actionID, tool)
	}

	if action.ContextKey != contextKey {
		return nil, fmt.Errorf("action_id %q does not match current chat context", actionID)
	}

	hash, err := payloadHash(payload)
	if err != nil {
		return nil, err
	}

	if hash != action.PayloadHash {
		return nil, fmt.Errorf("action_id %q payload does not match pending action", actionID)
	}

	return action, nil
}

func (m *confirmationManager) Consume(actionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.actions, actionID)
}

func (m *confirmationManager) purgeExpiredLocked(now time.Time) {
	for id, action := range m.actions {
		if now.After(action.ExpiresAt) {
			delete(m.actions, id)
		}
	}
}

func payloadHash(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal confirmation payload: %w", err)
	}

	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
