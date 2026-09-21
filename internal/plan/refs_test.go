package plan

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Shaped after real `terraform show -json` output: a depends_on edge, a direct
// attribute reference, an edge inside a module, and one that crosses a module
// boundary through a variable.
const refsShow = `{
  "resource_changes": [
    {"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","name":"logs","change":{"actions":["create"],"after":{}}},
    {"address":"aws_db_instance.orders","type":"aws_db_instance","name":"orders","change":{"actions":["create"],"after":{}}},
    {"address":"aws_s3_bucket_public_access_block.logs","type":"aws_s3_bucket_public_access_block","name":"logs","change":{"actions":["create"],"after":{}}},
    {"address":"module.db.null_resource.inner","module_address":"module.db","type":"null_resource","name":"inner","change":{"actions":["create"],"after":{}}},
    {"address":"module.db.null_resource.downstream","module_address":"module.db","type":"null_resource","name":"downstream","change":{"actions":["create"],"after":{}}},
    {"address":"aws_sqs_queue.untouched","type":"aws_sqs_queue","name":"untouched","change":{"actions":["create"],"after":{}}}
  ],
  "configuration": {"root_module": {
    "resources": [
      {"address":"aws_s3_bucket_public_access_block.logs",
       "depends_on":["aws_db_instance.orders"],
       "expressions":{"bucket":{"references":["aws_s3_bucket.logs.id","aws_s3_bucket.logs"]},
                      "rule":[{"nested":{"references":["aws_sqs_queue.untouched"]}}]}},
      {"address":"aws_sqs_queue.untouched","expressions":{"name":{"constant_value":"q"}}}
    ],
    "module_calls": {"db": {
      "expressions":{"bucket_id":{"references":["aws_s3_bucket.logs.id","aws_s3_bucket.logs"]},
                     "unused":{"constant_value":"x"}},
      "module":{"resources":[
        {"address":"null_resource.inner","expressions":{"triggers":{"references":["var.bucket_id"]}}},
        {"address":"null_resource.downstream","expressions":{"triggers":{"references":["null_resource.inner.id","null_resource.inner"]}}}
      ]}
    }}
  }}
}`

func TestExtractResolvesReferences(t *testing.T) {
	p, err := Extract([]byte(refsShow), "prd")
	require.NoError(t, err)

	pab := byAddress(p, "aws_s3_bucket_public_access_block.logs")
	require.Equal(t, []string{"aws_db_instance.orders", "aws_s3_bucket.logs", "aws_sqs_queue.untouched"}, pab.Refs,
		"depends_on, a direct reference and one nested inside a block should all resolve")

	// Crossing a module boundary: the module call binds bucket_id in the caller's
	// scope, and the inner resource reaches it as var.bucket_id.
	require.Equal(t, []string{"aws_s3_bucket.logs"}, byAddress(p, "module.db.null_resource.inner").Refs)
	require.Equal(t, []string{"module.db.null_resource.inner"}, byAddress(p, "module.db.null_resource.downstream").Refs)

	require.Equal(t, []string{"aws_s3_bucket_public_access_block.logs", "module.db.null_resource.inner"},
		byAddress(p, "aws_s3_bucket.logs").ReferredBy)
	require.Empty(t, byAddress(p, "aws_s3_bucket.logs").Refs)
}

func TestExtractDropsReferencesOutsideThePlan(t *testing.T) {
	show := `{"resource_changes":[{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","name":"logs","change":{"actions":["create"],"after":{}}}],
	  "configuration":{"root_module":{"resources":[
	    {"address":"aws_s3_bucket.logs","expressions":{"bucket":{"references":["aws_kms_key.absent.arn","var.name"]}}}]}}}`
	p, err := Extract([]byte(show), "prd")
	require.NoError(t, err)
	// A reference to something the plan does not change says nothing about this change.
	require.Empty(t, byAddress(p, "aws_s3_bucket.logs").Refs)
}

func TestExtractWithoutConfigurationSection(t *testing.T) {
	p, err := Extract([]byte(`{"resource_changes":[{"address":"a.b","type":"a","name":"b","change":{"actions":["create"],"after":{}}}]}`), "prd")
	require.NoError(t, err)
	require.Empty(t, p.Resources[0].Refs)
	require.Empty(t, p.Resources[0].ReferredBy)
}
