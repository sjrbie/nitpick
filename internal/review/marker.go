// Package review implements the local-first comment loop: scraping inline
// "nit:" markers out of source files, translating them to pull-request review
// comments, stripping them back out, and stashing what was posted.
package review

import (
	"regexp"
	"strings"
)

// kind distinguishes a marker that occupies its own line from one trailing a
// line of code.
type kind int

const (
	ownLine  kind = iota // "    // nit: ..."  -> the whole line is a marker
	trailing             // "code() // nit: ..." -> marker follows real code
)

// Marker is a single inline review note ready to be posted. Line is the target
// line number in the committed file (i.e. with own-line markers removed), which
// is what a forge review comment anchors against.
type Marker struct {
	Path string
	Line int
	Body string

	// strip metadata (unexported: only this package rewrites files)
	startIdx int // 0-based index of the first marker line in the working tree
	endIdx   int // exclusive end of the marker block
	kind     kind
	keepCode string // for trailing markers, the code to preserve
}

// markerRe matches a line containing a "nit:" marker behind any common
// line/block comment leader. Group 1 is everything before the leader (used to
// tell own-line from trailing and to preserve code); group 3 is the note body.
var markerRe = regexp.MustCompile(`(?i)^(.*?)(//|/\*|<!--|#|--|;)[ \t]*nit:[ \t]?(.*?)[ \t]*(?:\*/|-->)?[ \t]*$`)

// lineInfo is the parsed marker state of a single source line.
type lineInfo struct {
	isMarker bool
	own      bool
	blank    bool
	body     string
	keepCode string
}

func classify(line string) lineInfo {
	m := markerRe.FindStringSubmatch(line)
	if m == nil {
		return lineInfo{blank: strings.TrimSpace(line) == ""}
	}
	pre := m[1]
	body := m[3]
	if strings.TrimSpace(pre) == "" {
		return lineInfo{isMarker: true, own: true, body: body}
	}
	return lineInfo{isMarker: true, own: false, body: body, keepCode: strings.TrimRight(pre, " \t")}
}

// ParseFile extracts markers from a file's content. Own-line markers on
// consecutive lines are merged into one note anchored to the next code line
// below them; a trailing marker anchors to its own line. Line numbers are
// translated to the committed file (own-line markers removed), assuming those
// markers are the only local additions to the file.
func ParseFile(path, content string) []Marker {
	lines := strings.Split(content, "\n")
	infos := make([]lineInfo, len(lines))
	for i, l := range lines {
		infos[i] = classify(l)
	}

	// ownBefore[i] = number of own-line marker lines strictly above index i.
	ownBefore := make([]int, len(lines)+1)
	for i := range lines {
		ownBefore[i+1] = ownBefore[i]
		if infos[i].isMarker && infos[i].own {
			ownBefore[i+1]++
		}
	}
	// committedLine maps a 0-based working-tree index to its 1-based line
	// number once own-line markers are removed.
	committedLine := func(idx int) int { return idx + 1 - ownBefore[idx] }

	var markers []Marker
	for i := 0; i < len(lines); i++ {
		info := infos[i]
		if !info.isMarker {
			continue
		}
		if info.own {
			// Gather the consecutive own-line block.
			start := i
			var bodies []string
			for i < len(lines) && infos[i].isMarker && infos[i].own {
				bodies = append(bodies, infos[i].body)
				i++
			}
			end := i // exclusive
			anchor := anchorFor(infos, end, start)
			if anchor < 0 {
				continue // nothing to attach to
			}
			markers = append(markers, Marker{
				Path:     path,
				Line:     committedLine(anchor),
				Body:     joinBody(bodies),
				startIdx: start,
				endIdx:   end,
				kind:     ownLine,
			})
			i = end - 1 // for-loop will ++ back to end
			continue
		}
		// Trailing marker: anchors to its own line, code preserved.
		markers = append(markers, Marker{
			Path:     path,
			Line:     committedLine(i),
			Body:     info.body,
			startIdx: i,
			endIdx:   i + 1,
			kind:     trailing,
			keepCode: info.keepCode,
		})
	}
	return markers
}

// anchorFor picks the code line an own-line block attaches to: scanning down
// from end, the first line that survives stripping (code, or a trailing-marker
// line whose code is preserved), skipping blanks and stopping at another
// own-line marker block. Failing that, it scans up from start for the nearest
// such line. Returns -1 when none exists. Own-line markers are excluded because
// they are removed from the committed file.
func anchorFor(infos []lineInfo, end, start int) int {
	for k := end; k < len(infos); k++ {
		if infos[k].isMarker && infos[k].own {
			break // reached a different own-line block
		}
		if !infos[k].blank {
			return k
		}
	}
	for k := start - 1; k >= 0; k-- {
		if infos[k].isMarker && infos[k].own {
			continue // skip lines that will be removed
		}
		if !infos[k].blank {
			return k
		}
	}
	return -1
}

func joinBody(bodies []string) string {
	// Drop trailing empties so "// nit:" alone doesn't add blank lines.
	for len(bodies) > 0 && strings.TrimSpace(bodies[len(bodies)-1]) == "" {
		bodies = bodies[:len(bodies)-1]
	}
	return strings.Join(bodies, "\n")
}
