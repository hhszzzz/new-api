package service

import (
	"encoding/base64"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// A client can inline its real payload as base64 — a script, a document, a
// whole conversation — and a wordlist or a classifier reading the encoded form
// sees only noise. The expansion below rewrites such a run into the text it
// carries before anything is matched or submitted, so the gate reads what the
// upstream model will actually be given.
//
// The rules are deliberately narrow, because a false expansion corrupts the text
// an operator reads back and the text the audit node scores:
//
//   - Only runs of the base64 alphabet at least promptAuditBase64MinRun bytes
//     long are considered. Shorter runs are ordinary words and identifiers.
//   - Decoded bytes must be valid UTF-8, at least
//     promptAuditBase64PrintableMin printable, and contain at least one letter.
//     Images, archives, key material, hashes, and random IDs all fail this, so
//     they stay exactly as the client sent them.
//   - Expansion is capped by passes, runs, and total decoded bytes, so a
//     hand-crafted input cannot make the gate expand without bound.
const (
	promptAuditBase64MinRun       = 24
	promptAuditBase64MaxPasses    = 3
	promptAuditBase64RunsPerPass  = 16
	promptAuditBase64MaxDecoded   = 64 * 1024
	promptAuditBase64PrintableMin = 0.9
)

func isPromptAuditBase64Byte(value byte) bool {
	switch {
	case value >= 'A' && value <= 'Z', value >= 'a' && value <= 'z', value >= '0' && value <= '9':
		return true
	case value == '+', value == '/', value == '-', value == '_', value == '=':
		return true
	}
	return false
}

// decodePromptAuditBase64Run tries the four standard spellings of one run: the
// standard and URL alphabets, each with and without padding. A run is only
// accepted when it decodes to text a human could have written.
func decodePromptAuditBase64Run(run string) (string, bool) {
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(run)
		if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) {
			continue
		}
		text := string(decoded)
		if !promptAuditBase64LooksTextual(text) {
			continue
		}
		return text, true
	}
	return "", false
}

func promptAuditBase64LooksTextual(text string) bool {
	total, printable, letters := 0, 0, 0
	for _, value := range text {
		total++
		if unicode.IsPrint(value) {
			printable++
		}
		if unicode.IsLetter(value) {
			letters++
		}
	}
	if total == 0 || letters == 0 {
		return false
	}
	return float64(printable) >= promptAuditBase64PrintableMin*float64(total)
}

// expandPromptAuditBase64Pass rewrites one layer of encoded runs. It returns the
// rewritten text, whether anything changed, and how many decoded bytes the pass
// spent against the caller's budget.
func expandPromptAuditBase64Pass(text string, maxRuns, budget int) (string, bool, int) {
	var builder strings.Builder
	builder.Grow(len(text))
	runs, spent, changed := 0, 0, false
	for index := 0; index < len(text); {
		if !isPromptAuditBase64Byte(text[index]) {
			builder.WriteByte(text[index])
			index++
			continue
		}
		end := index
		for end < len(text) && isPromptAuditBase64Byte(text[end]) {
			end++
		}
		run := text[index:end]
		if len(run) >= promptAuditBase64MinRun && runs < maxRuns && spent < budget {
			if decoded, ok := decodePromptAuditBase64Run(run); ok && len(decoded) <= budget-spent {
				builder.WriteString(decoded)
				runs++
				spent += len(decoded)
				changed = true
				index = end
				continue
			}
		}
		builder.WriteString(run)
		index = end
	}
	return builder.String(), changed, spent
}

// expandPromptAuditBase64 returns the text with every base64 run it can
// confidently read replaced by what that run carries. The original is returned
// unchanged when nothing qualifies, so a caller can compare the two to tell
// whether an expansion happened.
func expandPromptAuditBase64(text string) string {
	if len(text) < promptAuditBase64MinRun {
		return text
	}
	budget := promptAuditBase64MaxDecoded
	for pass := 0; pass < promptAuditBase64MaxPasses; pass++ {
		expanded, changed, spent := expandPromptAuditBase64Pass(text, promptAuditBase64RunsPerPass, budget)
		budget -= spent
		text = expanded
		if !changed || budget <= 0 {
			break
		}
	}
	return text
}

// expandPromptAuditSnapshotBase64 copies a snapshot and expands the text of
// every segment. The caller's snapshot is left untouched: it is still the raw
// text the request carried, which is what gets stored as the full prompt. The
// second result reports whether anything was actually rewritten.
func expandPromptAuditSnapshotBase64(snapshot dto.PromptAuditSnapshot) (dto.PromptAuditSnapshot, bool) {
	if len(snapshot.Segments) == 0 {
		return snapshot, false
	}
	segments := make([]dto.PromptAuditSegment, len(snapshot.Segments))
	copy(segments, snapshot.Segments)
	changed := false
	for index := range segments {
		expanded := expandPromptAuditBase64(segments[index].Text)
		if expanded != segments[index].Text {
			segments[index].Text = expanded
			changed = true
		}
	}
	if !changed {
		return snapshot, false
	}
	snapshot.Segments = segments
	return snapshot, true
}

// promptAuditJoinedText renders a snapshot and an output the way the audit
// service joins them, so a raw copy and an expanded copy can be compared.
func promptAuditJoinedText(snapshot dto.PromptAuditSnapshot, output string) string {
	texts := make([]string, 0, len(snapshot.Segments)+1)
	for _, segment := range snapshot.Segments {
		if strings.TrimSpace(segment.Text) != "" {
			texts = append(texts, segment.Text)
		}
	}
	if strings.TrimSpace(output) != "" {
		texts = append(texts, output)
	}
	return strings.Join(texts, "\n\n")
}
