package plan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T) *Plan {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "show-basic.json"))
	require.NoError(t, err)
	p, err := Extract(raw, "aws-prd")
	require.NoError(t, err)
	return p
}

func byAddress(p *Plan, addr string) Resource {
	for _, r := range p.Resources {
		if r.Address == addr {
			return r
		}
	}
	return Resource{}
}

func TestExtractDropsNoop(t *testing.T) {
	p := loadFixture(t)
	require.Equal(t, "aws-prd", p.Target)
	require.Len(t, p.Resources, 6)
	require.Empty(t, byAddress(p, "aws_vpc.main").Address)
}

func TestExtractCounts(t *testing.T) {
	p := loadFixture(t)
	require.Equal(t, Counts{Add: 1, Change: 2, Destroy: 1, Replace: 1, Import: 2}, p.Counts)
}

func TestExtractKeepsImportOnlyNoop(t *testing.T) {
	p := loadFixture(t)
	r := byAddress(p, "aws_s3_bucket.adopted")
	require.Equal(t, []string{"no-op"}, r.Actions)
	require.Equal(t, map[string]any{"bucket": "adopted"}, r.After)
	require.Nil(t, r.ChangedKeys)
}

func TestExtractChangedKeysOnlyForUpdateAndReplace(t *testing.T) {
	p := loadFixture(t)
	require.Equal(t, []string{"force_destroy"}, byAddress(p, "aws_s3_bucket.logs").ChangedKeys)
	require.Equal(t, []string{"instance_class", "password"}, byAddress(p, "module.db.aws_db_instance.main").ChangedKeys)
	require.Nil(t, byAddress(p, "aws_sqs_queue.jobs").ChangedKeys)
	require.Nil(t, byAddress(p, "aws_iam_user.alice").ChangedKeys)
}

func TestExtractStripsSensitiveAndBefore(t *testing.T) {
	p := loadFixture(t)
	db := byAddress(p, "module.db.aws_db_instance.main")
	require.Equal(t, "module.db", db.ModuleAddress)
	require.NotContains(t, db.After, "password")
	require.Equal(t, "db.t3.medium", db.After["instance_class"])
	require.Equal(t, []string{"delete", "create"}, db.Actions)
}

func TestExtractDeleteHasNilAfter(t *testing.T) {
	p := loadFixture(t)
	require.Nil(t, byAddress(p, "aws_iam_user.alice").After)
}

func TestHasChangesAndDigest(t *testing.T) {
	p := loadFixture(t)
	require.True(t, p.HasChanges())
	require.False(t, (&Plan{Target: "x"}).HasChanges())
	d1 := p.Digest()
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, d1)
	require.Equal(t, d1, loadFixture(t).Digest())
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := loadFixture(t)
	path := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, p.Save(path))
	got, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, p, got)
}

func TestExtractDropsAfterOnUnparsableSensitive(t *testing.T) {
	raw := `{"resource_changes":[{"address":"x","type":"t","name":"n","change":{"actions":["update"],"before":{"a":1},"after":{"a":2},"after_sensitive":"garbage"}}]}`
	p, err := Extract([]byte(raw), "x")
	require.NoError(t, err)
	require.Nil(t, byAddress(p, "x").After)
}

func TestExtractPopulatesUnknownKeys(t *testing.T) {
	raw := `{"resource_changes":[{"address":"acme_widget.this","type":"acme_widget","name":"this","change":{"actions":["update"],"before":{"name":"old","encryption_config":null},"after":{"name":"new","encryption_config":null},"after_sensitive":{},"after_unknown":{"encryption_config":true}}}]}`
	p, err := Extract([]byte(raw), "x")
	require.NoError(t, err)
	r := byAddress(p, "acme_widget.this")
	require.Equal(t, []string{"encryption_config"}, r.UnknownKeys)
	require.Nil(t, r.After["encryption_config"])
}

func extractOne(t *testing.T, change string) Resource {
	t.Helper()
	raw := `{"resource_changes":[{"address":"acme_job.this","type":"acme_job","name":"this","change":` + change + `}]}`
	p, err := Extract([]byte(raw), "x")
	require.NoError(t, err)
	return byAddress(p, "acme_job.this")
}

// Terraform emits the shape of nested blocks in after_sensitive even when nothing
// inside is sensitive; that must not hide the block from the reviewer.
func TestExtractKeepsNestedBlocksWhenNothingIsSensitive(t *testing.T) {
	r := extractOne(t, `{"actions":["create"],"before":null,
		"after":{"name":"job","template":[{"containers":[{"env":[{"name":"DB","value_source":[{"secret_key_ref":[{"secret":"db"}]}]}]}]}]},
		"after_sensitive":{"template":[{"containers":[{"env":[{"value_source":[{"secret_key_ref":[{}]}]}]}]}]}}`)
	require.Contains(t, r.After, "template")
}

func TestExtractRedactsOnlyNestedSensitivePositions(t *testing.T) {
	r := extractOne(t, `{"actions":["create"],"before":null,
		"after":{"name":"job","template":[{"containers":[{"image":"app","env":[{"name":"A","value":"s3cret"},{"name":"B","value":"plain"}]}]}],"tokens":["t1","t2"]},
		"after_sensitive":{"template":[{"containers":[{"env":[{"value":true},{}]}]}],"tokens":[false,true]}}`)
	require.Equal(t, map[string]any{
		"name": "job",
		"template": []any{map[string]any{"containers": []any{map[string]any{
			"image": "app",
			"env": []any{
				map[string]any{"name": "A"},
				map[string]any{"name": "B", "value": "plain"},
			},
		}}}},
		"tokens": []any{"t1", "(sensitive)"},
	}, r.After)
}

func TestExtractDropsTopLevelSensitiveAttribute(t *testing.T) {
	r := extractOne(t, `{"actions":["create"],"before":null,"after":{"name":"job","password":"s3cret"},"after_sensitive":{"password":true}}`)
	require.Equal(t, map[string]any{"name": "job"}, r.After)
}

func TestExtractUnknownKeysNeedsATrueFlag(t *testing.T) {
	r := extractOne(t, `{"actions":["update"],"before":{"a":1,"template":[{}]},"after":{"a":2,"template":[{}],"id":null,"nested":[{"x":null}]},
		"after_sensitive":{},"after_unknown":{"template":[{}],"id":true,"nested":[{"x":true}]}}`)
	require.Equal(t, []string{"id", "nested"}, r.UnknownKeys)
}

func TestExtractRejectsInvalidJSON(t *testing.T) {
	_, err := Extract([]byte("{"), "x")
	require.Error(t, err)
}

func TestExtractActionReasonAndReplacePaths(t *testing.T) {
	raw := []byte(`{"resource_changes":[{
		"address":"aws_db_instance.orders","type":"aws_db_instance","name":"orders",
		"provider_name":"registry.terraform.io/hashicorp/aws",
		"action_reason":"replace_because_cannot_update",
		"change":{"actions":["delete","create"],"before":{"engine_version":"14"},
			"after":{"engine_version":"15"},
			"replace_paths":[["engine_version"],["restore_to_point_in_time",0,"source"]]}}]}`)
	p, err := Extract(raw, "prd")
	require.NoError(t, err)
	r := byAddress(p, "aws_db_instance.orders")
	require.Equal(t, "replace_because_cannot_update", r.ActionReason)
	require.Equal(t, []string{"engine_version", "restore_to_point_in_time.0.source"}, r.ReplacePaths)
}
