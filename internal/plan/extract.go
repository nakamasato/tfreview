package plan

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

type showJSON struct {
	ResourceChanges []struct {
		Address       string `json:"address"`
		ModuleAddress string `json:"module_address"`
		Type          string `json:"type"`
		Name          string `json:"name"`
		ProviderName  string `json:"provider_name"`
		Change        struct {
			Actions        []string        `json:"actions"`
			Before         map[string]any  `json:"before"`
			After          map[string]any  `json:"after"`
			AfterSensitive json.RawMessage `json:"after_sensitive"`
			Importing      json.RawMessage `json:"importing"`
		} `json:"change"`
	} `json:"resource_changes"`
}

func Extract(raw []byte, target string) (*Plan, error) {
	var show showJSON
	if err := json.Unmarshal(raw, &show); err != nil {
		return nil, fmt.Errorf("parse terraform show -json: %w", err)
	}
	p := &Plan{Target: target, Resources: []Resource{}}
	for _, rc := range show.ResourceChanges {
		importing := len(rc.Change.Importing) > 0 && string(rc.Change.Importing) != "null"
		kind := classify(rc.Change.Actions)
		// no-op/read are ordinarily dropped as noise, but a no-op/read paired with
		// `importing` is terraform adopting an existing resource into state — a
		// real event worth keeping even though nothing about the resource changes.
		if kind == "" && !importing {
			continue
		}
		switch kind {
		case "add":
			p.Counts.Add++
		case "change":
			p.Counts.Change++
		case "destroy":
			p.Counts.Destroy++
		case "replace":
			p.Counts.Replace++
		}
		if importing {
			p.Counts.Import++
		}
		r := Resource{
			Address:       rc.Address,
			Type:          rc.Type,
			Name:          rc.Name,
			ModuleAddress: rc.ModuleAddress,
			ProviderName:  rc.ProviderName,
			Actions:       rc.Change.Actions,
			After:         stripSensitive(rc.Change.After, rc.Change.AfterSensitive),
		}
		if kind == "change" || kind == "replace" {
			r.ChangedKeys = changedKeys(rc.Change.Before, rc.Change.After)
		}
		p.Resources = append(p.Resources, r)
	}
	return p, nil
}

func classify(actions []string) string {
	switch {
	case len(actions) == 2:
		return "replace"
	case len(actions) == 1 && actions[0] == "create":
		return "add"
	case len(actions) == 1 && actions[0] == "update":
		return "change"
	case len(actions) == 1 && actions[0] == "delete":
		return "destroy"
	}
	return ""
}

// after_sensitive は after と同じ形で、sensitive な位置に true が立つ。
// トップレベルの属性だけ見る（ネストした sensitive は属性ごと落とす）。
func stripSensitive(after map[string]any, sensitive json.RawMessage) map[string]any {
	if after == nil {
		return nil
	}
	var flags map[string]any
	_ = json.Unmarshal(sensitive, &flags)
	out := make(map[string]any, len(after))
	for k, v := range after {
		if f, ok := flags[k]; ok && isSensitive(f) {
			continue
		}
		out[k] = v
	}
	return out
}

func isSensitive(flag any) bool {
	switch f := flag.(type) {
	case bool:
		return f
	case map[string]any:
		return len(f) > 0
	case []any:
		return len(f) > 0
	}
	return false
}

func changedKeys(before, after map[string]any) []string {
	keys := map[string]struct{}{}
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	var out []string
	for k := range keys {
		if !reflect.DeepEqual(before[k], after[k]) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
