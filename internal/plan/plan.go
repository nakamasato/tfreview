// Package plan handles a Plan reduced from `terraform show -json` to only what judging needs.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
)

type Counts struct {
	Add     int `json:"add"`
	Change  int `json:"change"`
	Destroy int `json:"destroy"`
	Replace int `json:"replace"`
	Import  int `json:"import"`
}

type Resource struct {
	Address       string         `json:"address"`
	Type          string         `json:"type"`
	Name          string         `json:"name"`
	ModuleAddress string         `json:"module_address"`
	ProviderName  string         `json:"provider_name"`
	Actions       []string       `json:"actions"`
	After         map[string]any `json:"after"`
	ChangedKeys   []string       `json:"changed_keys,omitempty"`
}

type Plan struct {
	Target    string     `json:"target"`
	Counts    Counts     `json:"counts"`
	Resources []Resource `json:"resources"`
}

func (p *Plan) HasChanges() bool { return len(p.Resources) > 0 }

// Digest is used as the state key. encoding/json sorts map keys, so the same
// content always marshals to the same bytes.
func (p *Plan) Digest() string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (p *Plan) Save(path string) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func Load(path string) (*Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
