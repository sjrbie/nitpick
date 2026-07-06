package review

import "testing"

// mk is a compact expected-marker for table tests.
type mk struct {
	line int
	body string
}

func TestParseFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []mk
	}{
		{
			name:    "own-line marker anchors to next code line",
			content: "package p\n\n// nit: add a nil check\nfunc F() {}\n",
			want:    []mk{{line: 3, body: "add a nil check"}},
		},
		{
			name:    "trailing marker anchors to its own line",
			content: "x := f()  // nit: rename x\ny := g()\n",
			want:    []mk{{line: 1, body: "rename x"}},
		},
		{
			name:    "consecutive own-line markers merge",
			content: "// nit: first point\n// nit: second point\ncall()\n",
			want:    []mk{{line: 1, body: "first point\nsecond point"}},
		},
		{
			name:    "hash leader (python/shell)",
			content: "# nit: use a set here\nvalues = []\n",
			want:    []mk{{line: 1, body: "use a set here"}},
		},
		{
			name:    "block-comment leaders",
			content: "/* nit: c-style */\nint x;\n<!-- nit: html style -->\n<div>\n",
			want:    []mk{{line: 1, body: "c-style"}, {line: 2, body: "html style"}},
		},
		{
			name:    "blank line between marker and code is skipped",
			content: "// nit: note\n\nfunc Z() {}\n",
			want:    []mk{{line: 2, body: "note"}},
		},
		{
			name:    "marker at EOF falls back to previous code line",
			content: "func A() {}\n// nit: revisit A\n",
			want:    []mk{{line: 1, body: "revisit A"}},
		},
		{
			name:    "body may contain comment-like text",
			content: "// nit: prefer a // b style here\ncode()\n",
			want:    []mk{{line: 1, body: "prefer a // b style here"}},
		},
		{
			name:    "no markers",
			content: "package p\nfunc F() {}\n",
			want:    nil,
		},
		{
			name:    "two own-line markers on different code lines",
			content: "// nit: one\nlineA()\n// nit: two\nlineB()\n",
			want:    []mk{{line: 1, body: "one"}, {line: 2, body: "two"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFile("f.go", tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d markers, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				if got[i].Line != w.line || got[i].Body != w.body {
					t.Errorf("marker %d = {line:%d body:%q}, want {line:%d body:%q}",
						i, got[i].Line, got[i].Body, w.line, w.body)
				}
				if got[i].Path != "f.go" {
					t.Errorf("marker %d path = %q, want f.go", i, got[i].Path)
				}
			}
		})
	}
}
