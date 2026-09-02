// Package plan は terraform show -json を判定に必要な形に絞った Plan を扱う。
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

// Digest は state のキーに使う。encoding/json は map のキーをソートするので、
// 同じ内容なら同じ bytes になる。
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
