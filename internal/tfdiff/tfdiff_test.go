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
