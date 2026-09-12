package tfdiff

import (
	"strings"
	"testing"
)

const raw = `diff --git a/main.tf b/main.tf
--- a/main.tf
+++ b/main.tf
@@ -1,3 +1,4 @@
 resource "aws_db_instance" "main" {
   engine = "postgres"
-  deletion_protection = true
+  deletion_protection = false
 }
diff --git a/other.tf b/other.tf
--- a/other.tf
+++ b/other.tf
@@ -10,3 +10,4 @@
 resource "aws_sns_topic" "alerts" {
   name = "alerts"
+  display_name = "Alerts"
 }
diff --git a/vars.tf b/vars.tf
--- a/vars.tf
+++ b/vars.tf
@@ -1,2 +1,3 @@
 variable "env" {
+  default = "dev"
 }
diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1,1 +1,2 @@
 # docs
+more docs
`

func TestReduceDropsNonTerraformFiles(t *testing.T) {
	d := Reduce([]byte(raw), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if strings.Contains(d.Text, "README.md") || strings.Contains(d.Text, "more docs") {
		t.Errorf("README survived reduction:\n%s", d.Text)
	}
}

func TestReduceKeepsPlanResourceTypesAndVariables(t *testing.T) {
	d := Reduce([]byte(raw), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if !strings.Contains(d.Text, "deletion_protection = false") {
		t.Errorf("the changed resource type was dropped:\n%s", d.Text)
	}
	if !strings.Contains(d.Text, `variable "env"`) {
		t.Errorf("variable block was dropped:\n%s", d.Text)
	}
	if strings.Contains(d.Text, "aws_sns_topic") {
		t.Errorf("a resource type absent from the plan survived:\n%s", d.Text)
	}
	if !strings.Contains(d.Summary, "omitted") {
		t.Errorf("Summary = %q, want it to mention omitted hunks", d.Summary)
	}
}

func TestReduceAlwaysKeepsLifecycleAndSuppression(t *testing.T) {
	in := `diff --git a/x.tf b/x.tf
--- a/x.tf
+++ b/x.tf
@@ -1,3 +1,5 @@
 resource "aws_sns_topic" "alerts" {
+  lifecycle {
+    prevent_destroy = false
+  }
 }
diff --git a/y.tf b/y.tf
--- a/y.tf
+++ b/y.tf
@@ -1,2 +1,3 @@
 resource "aws_sns_topic" "other" {
+  # trivy:ignore:AVD-AWS-0001
 }
`
	d := Reduce([]byte(in), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if !strings.Contains(d.Text, "prevent_destroy") {
		t.Errorf("lifecycle hunk was dropped:\n%s", d.Text)
	}
	if !strings.Contains(d.Text, "trivy:ignore") {
		t.Errorf("suppression hunk was dropped:\n%s", d.Text)
	}
}

func TestReduceRespectsBudget(t *testing.T) {
	d := Reduce([]byte(raw), []string{"aws_db_instance"}, Budget{MaxDiffChars: 10, MaxInputChars: 200000})
	if len(d.Text) > 10 {
		t.Errorf("Text is %d chars, want <= 10", len(d.Text))
	}
	d2 := Reduce([]byte(raw), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 100, Used: 100})
	if !d2.IsEmpty() {
		t.Errorf("Text = %q, want empty when the input budget is exhausted", d2.Text)
	}
}

// TestReduceTrimsWholeHunksNotPartialText pins whole-hunk trimming: a naive
// character-truncating implementation would also satisfy TestReduceRespectsBudget's
// "<= 10 chars" assertion, so this checks that a budget sized to fit exactly one
// surviving hunk yields that hunk byte-for-byte, not a truncated prefix of the
// combined text.
func TestReduceTrimsWholeHunksNotPartialText(t *testing.T) {
	// The trailing "diff --git" line for an unrelated dropped file mirrors how vars.tf
	// appears in raw (followed by another file), so the reconstructed hunk text ends
	// the same way in both cases and this is a byte-for-byte comparison, not a
	// same-content-modulo-trailing-newline one.
	varsOnly := `diff --git a/vars.tf b/vars.tf
--- a/vars.tf
+++ b/vars.tf
@@ -1,2 +1,3 @@
 variable "env" {
+  default = "dev"
 }
diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1,1 +1,2 @@
 # docs
+more docs
`
	want := Reduce([]byte(varsOnly), nil, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	d := Reduce([]byte(raw), []string{"aws_db_instance"}, Budget{MaxDiffChars: len(want.Text), MaxInputChars: 200000})
	if d.Text != want.Text {
		t.Errorf("Text = %q, want the variable hunk kept whole and byte-identical:\n%q", d.Text, want.Text)
	}
}

// TestReduceSummaryWhenNoHunksFound: when every file in the diff is dropped for its
// extension, the result is empty, and the summary must say so rather than falsely
// claiming the full diff is shown.
func TestReduceSummaryWhenNoHunksFound(t *testing.T) {
	in := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1,1 +1,2 @@
 # docs
+more docs
`
	d := Reduce([]byte(in), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if !d.IsEmpty() {
		t.Errorf("Text = %q, want empty (no Terraform files in the diff)", d.Text)
	}
	if strings.Contains(d.Summary, "full Terraform diff is shown") {
		t.Errorf("Summary = %q, want it to say no Terraform hunks were found, not that the full diff is shown", d.Summary)
	}
}

// TestReduceKeepsInheritedAttributionMatchingPlanType covers the common case of a
// small edit deep inside a large resource block: the second hunk's own context lines
// never repeat the "resource ..." header, so its block comes from the first hunk.
// Since the inherited type matches planTypes, it must be kept regardless.
func TestReduceKeepsInheritedAttributionMatchingPlanType(t *testing.T) {
	in := `diff --git a/main.tf b/main.tf
--- a/main.tf
+++ b/main.tf
@@ -1,4 +1,4 @@
 resource "aws_db_instance" "main" {
   engine = "postgres"
-  deletion_protection = true
+  deletion_protection = false
 }
@@ -20,3 +20,4 @@
   tags = {
+    Name = "main-db"
   }
 }
`
	d := Reduce([]byte(in), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if !strings.Contains(d.Text, `Name = "main-db"`) {
		t.Errorf("a hunk with inherited (not own-context) attribution to a plan resource type was dropped:\n%s", d.Text)
	}
}

// TestReduceKeepsInheritedAttributionToExcludedTypeAsUncertain is the sharpest
// reproduction of Finding 2: a hunk inherits a resource type absent from planTypes
// from an earlier hunk in the same file, without its own context ever repeating the
// header. It cannot be confidently excluded (the intervening lines could belong to a
// different, unheadered block), so Ruling A keeps it, and the summary must report it
// as unclassifiable rather than as a confident exclusion.
func TestReduceKeepsInheritedAttributionToExcludedTypeAsUncertain(t *testing.T) {
	in := `diff --git a/topic.tf b/topic.tf
--- a/topic.tf
+++ b/topic.tf
@@ -1,3 +1,3 @@
 resource "aws_sns_topic" "alerts" {
   name = "alerts"
 }
@@ -20,3 +20,4 @@
   tags = {
+    Owner = "platform"
   }
`
	d := Reduce([]byte(in), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if !strings.Contains(d.Text, `Owner = "platform"`) {
		t.Errorf("a hunk with inherited attribution to an excluded type was dropped instead of kept as uncertain:\n%s", d.Text)
	}
	// The first hunk (its own context has the "aws_sns_topic" header) is a genuine
	// confident exclusion and correctly counts toward the "omitted ... hunks" bucket;
	// only the *second* hunk's inherited, not-in-plan attribution must be reported as
	// uncertain rather than folded into that same confident-exclusion count.
	if !strings.Contains(d.Summary, "omitted 1 hunks across 1 files for resource types not in this plan") {
		t.Errorf("Summary = %q, want exactly the first hunk counted as a confident exclusion", d.Summary)
	}
	if !strings.Contains(d.Summary, "1 hunks whose enclosing block could not be determined were kept") {
		t.Errorf("Summary = %q, want the second, inherited-attribution hunk reported as unclassifiable", d.Summary)
	}
}

// TestReduceKeepsUnattributedHunkAndSummary covers a hunk with no recognisable
// enclosing block anywhere in its file yet (the first hunk in a file, or one whose
// 3-line context window misses the header). It must be kept, and reported in the
// summary as unclassifiable rather than as an excluded resource type.
func TestReduceKeepsUnattributedHunkAndSummary(t *testing.T) {
	in := `diff --git a/deep.tf b/deep.tf
--- a/deep.tf
+++ b/deep.tf
@@ -50,3 +50,4 @@
   some_field = "x"
+  other_field = "y"
 }
`
	d := Reduce([]byte(in), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if !strings.Contains(d.Text, `other_field = "y"`) {
		t.Errorf("a hunk with no recognizable enclosing block was dropped:\n%s", d.Text)
	}
	if strings.Contains(d.Summary, "for resource types not in this plan") {
		t.Errorf("Summary = %q, an unclassifiable hunk must not be reported as an excluded resource type", d.Summary)
	}
	if !strings.Contains(d.Summary, "could not be determined") {
		t.Errorf("Summary = %q, want it to report the hunk as unclassifiable", d.Summary)
	}
}

// TestReduceDropsConfidentlyExcludedResourceType pins that Ruling A (keep when
// uncertain) did not also turn off the original filter: a hunk whose own context
// names a resource type absent from planTypes is still dropped and still counted in
// the existing "omitted ... for resource types not in this plan" bucket.
func TestReduceDropsConfidentlyExcludedResourceType(t *testing.T) {
	d := Reduce([]byte(raw), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if strings.Contains(d.Text, "aws_sns_topic") {
		t.Errorf("a hunk confidently attributed to an excluded resource type survived:\n%s", d.Text)
	}
	if !strings.Contains(d.Summary, "omitted 1 hunks across 1 files for resource types not in this plan") {
		t.Errorf("Summary = %q, want the confident-exclusion bucket unaffected", d.Summary)
	}
}

// TestReduceHandlesQuotedFilePathWithSpace covers git's quoted diff --git line for a
// path containing a space; the file must still resolve and survive.
func TestReduceHandlesQuotedFilePathWithSpace(t *testing.T) {
	in := `diff --git "a/my file.tf" "b/my file.tf"
--- a/my file.tf
+++ b/my file.tf
@@ -1,2 +1,3 @@
 resource "aws_db_instance" "main" {
+  new_setting = true
 }
`
	d := Reduce([]byte(in), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if !strings.Contains(d.Text, "new_setting = true") {
		t.Errorf("a file with a quoted, space-containing path was dropped:\n%s", d.Text)
	}
}

// TestReduceResolvesDeletedFilePathFromMinusLine covers a deleted file, whose "+++"
// line reads "/dev/null" and carries no path; the extension check must fall back to
// the "--- a/..." line instead of dropping the file outright.
func TestReduceResolvesDeletedFilePathFromMinusLine(t *testing.T) {
	in := `diff --git a/old.tf b/old.tf
--- a/old.tf
+++ /dev/null
@@ -1,3 +0,0 @@
-resource "aws_db_instance" "main" {
-  engine = "postgres"
-}
`
	d := Reduce([]byte(in), []string{"aws_db_instance"}, Budget{MaxDiffChars: 100000, MaxInputChars: 200000})
	if !strings.Contains(d.Text, `engine = "postgres"`) {
		t.Errorf("a deleted .tf file's path was not resolved from the \"--- a/...\" line:\n%s", d.Text)
	}
}
