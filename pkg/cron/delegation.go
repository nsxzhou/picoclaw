package cron

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

const (
	delegationKeyFileName = "delegation.key"
	delegationVersionV1   = "v1"
)

// CronDelegation 保存定时任务执行时所需的用户态委托身份。
// 说明：为了支持长期定时任务，当前版本不设置过期时间，签名长期有效。
type CronDelegation struct {
	Version    string `json:"version"`
	IssuedAtMS int64  `json:"issued_at_ms"`
	SenderID   string `json:"sender_id"`
	Signature  string `json:"signature"`
}

func (cs *CronService) SignDelegation(
	jobID, channel, chatID, senderID string,
	now time.Time,
) (*CronDelegation, error) {
	jobID = strings.TrimSpace(jobID)
	channel = strings.TrimSpace(channel)
	chatID = strings.TrimSpace(chatID)
	senderID = strings.TrimSpace(senderID)
	if jobID == "" || senderID == "" {
		return nil, fmt.Errorf("delegation requires job_id and sender_id")
	}

	key, err := cs.getOrCreateDelegationKey()
	if err != nil {
		return nil, err
	}

	issuedAtMS := now.UnixMilli()
	msg := delegationSignMessage(
		delegationVersionV1,
		jobID,
		channel,
		chatID,
		senderID,
		issuedAtMS,
	)

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(msg))
	sig := hex.EncodeToString(mac.Sum(nil))

	return &CronDelegation{
		Version:    delegationVersionV1,
		IssuedAtMS: issuedAtMS,
		SenderID:   senderID,
		Signature:  sig,
	}, nil
}

func (cs *CronService) ValidateDelegation(
	d *CronDelegation,
	jobID, channel, chatID string,
) error {
	if d == nil {
		return fmt.Errorf("missing delegation")
	}
	if strings.TrimSpace(d.Version) != delegationVersionV1 {
		return fmt.Errorf("unsupported delegation version")
	}
	senderID := strings.TrimSpace(d.SenderID)
	if senderID == "" {
		return fmt.Errorf("delegation sender_id is empty")
	}
	if strings.TrimSpace(d.Signature) == "" {
		return fmt.Errorf("delegation signature is empty")
	}
	if d.IssuedAtMS <= 0 {
		return fmt.Errorf("delegation issued_at_ms is invalid")
	}

	key, err := cs.getOrCreateDelegationKey()
	if err != nil {
		return err
	}

	msg := delegationSignMessage(
		d.Version,
		strings.TrimSpace(jobID),
		strings.TrimSpace(channel),
		strings.TrimSpace(chatID),
		senderID,
		d.IssuedAtMS,
	)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(msg))
	expected := mac.Sum(nil)

	actual, err := hex.DecodeString(strings.TrimSpace(d.Signature))
	if err != nil {
		return fmt.Errorf("delegation signature is malformed")
	}
	if !hmac.Equal(actual, expected) {
		return fmt.Errorf("delegation signature mismatch")
	}
	return nil
}

func delegationSignMessage(
	version, jobID, channel, chatID, senderID string,
	issuedAtMS int64,
) string {
	return fmt.Sprintf(
		"%s|%s|%s|%s|%s|%d",
		version,
		jobID,
		channel,
		chatID,
		senderID,
		issuedAtMS,
	)
}

func (cs *CronService) getOrCreateDelegationKey() ([]byte, error) {
	cs.delegationKeyMu.Lock()
	defer cs.delegationKeyMu.Unlock()

	if len(cs.delegationKey) > 0 {
		keyCopy := make([]byte, len(cs.delegationKey))
		copy(keyCopy, cs.delegationKey)
		return keyCopy, nil
	}

	keyPath := cs.delegationKeyPath()
	if data, err := os.ReadFile(keyPath); err == nil {
		key := normalizeDelegationKey(data)
		if len(key) < 32 {
			return nil, fmt.Errorf("delegation key is too short")
		}
		cs.delegationKey = key
		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)
		return keyCopy, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read delegation key failed: %w", err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate delegation key failed: %w", err)
	}
	hexKey := hex.EncodeToString(key)
	if err := fileutil.WriteFileAtomic(keyPath, []byte(hexKey+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write delegation key failed: %w", err)
	}
	cs.delegationKey = key
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	return keyCopy, nil
}

func (cs *CronService) delegationKeyPath() string {
	return filepath.Join(filepath.Dir(cs.storePath), delegationKeyFileName)
}

func normalizeDelegationKey(raw []byte) []byte {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil
	}
	if decoded, err := hex.DecodeString(text); err == nil {
		return decoded
	}
	return []byte(text)
}
