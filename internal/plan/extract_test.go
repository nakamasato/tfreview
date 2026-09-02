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
	require.Len(t, p.Resources, 5)
	require.Empty(t, byAddress(p, "aws_vpc.main").Address)
}

func TestExtractCounts(t *testing.T) {
	p := loadFixture(t)
	require.Equal(t, Counts{Add: 1, Change: 2, Destroy: 1, Replace: 1, Import: 1}, p.Counts)
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

func TestExtractRejectsInvalidJSON(t *testing.T) {
	_, err := Extract([]byte("{"), "x")
	require.Error(t, err)
}
