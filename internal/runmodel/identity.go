package runmodel

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"
	"time"
)

type RunHandle struct {
	ID        string    `json:"runId"`
	StartedAt time.Time `json:"startedAt"`
	Output    string    `json:"output"`
}

func NewHandle(output string) (RunHandle, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return RunHandle{}, err
	}
	return RunHandle{ID: hex.EncodeToString(id[:]), StartedAt: time.Now().UTC(), Output: output}, nil
}

// TargetID scopes an input by its parent and slash-normalized relative path.
func TargetID(parentID, relativeInput string) string {
	key := path.Clean(strings.ReplaceAll(relativeInput, `\`, "/"))
	hash := sha256.Sum256([]byte(parentID + "\x00" + key))
	return "t" + hex.EncodeToString(hash[:8])
}
