package gitlocal

import (
	"reflect"
	"testing"
)

func TestParseUnifiedOldRanges(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want map[string][]LineRange
	}{
		{
			name: "single modified line",
			diff: `diff --git a/foo.go b/foo.go
index 111..222 100644
--- a/foo.go
+++ b/foo.go
@@ -10 +10 @@ func f() {
-	old
+	new
`,
			want: map[string][]LineRange{"foo.go": {{Start: 10, End: 10}}},
		},
		{
			name: "multi-line deletion and separate hunk",
			diff: `--- a/bar.go
+++ b/bar.go
@@ -5,3 +5,0 @@
-a
-b
-c
@@ -20,2 +18,1 @@
-x
-y
+z
`,
			want: map[string][]LineRange{"bar.go": {{Start: 5, End: 7}, {Start: 20, End: 21}}},
		},
		{
			name: "pure insertion ignored (old count 0)",
			diff: `--- a/baz.go
+++ b/baz.go
@@ -0,0 +1,2 @@
+one
+two
`,
			want: map[string][]LineRange{},
		},
		{
			name: "two files",
			diff: `--- a/one.go
+++ b/one.go
@@ -1 +1 @@
-x
+y
--- a/two.go
+++ b/two.go
@@ -3,2 +3,2 @@
-p
-q
+r
+s
`,
			want: map[string][]LineRange{
				"one.go": {{Start: 1, End: 1}},
				"two.go": {{Start: 3, End: 4}},
			},
		},
		{
			name: "new file to /dev/null path uses b-side name",
			diff: `--- /dev/null
+++ b/new.go
@@ -0,0 +1 @@
+hi
`,
			want: map[string][]LineRange{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUnifiedOldRanges(tt.diff)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLineRangeContains(t *testing.T) {
	r := LineRange{Start: 5, End: 8}
	for _, tc := range []struct {
		line int
		want bool
	}{{4, false}, {5, true}, {7, true}, {8, true}, {9, false}} {
		if got := r.Contains(tc.line); got != tc.want {
			t.Errorf("Contains(%d) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
