package mapbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"codeberg.org/nocfa/segments/internal/models"
)

var ordinalPrefix = regexp.MustCompile(`^\d+\s+`)
var bracketPrefix = regexp.MustCompile(`^\[[^\]]+\]\s+`)
var ticketNumberRE = regexp.MustCompile(`^(\d+)-.*\.md$`)

func normalizeLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}
func contentHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func projectSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
			continue
		}
		if (r == ' ' || r == '-' || r == '_') && b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
func titleSlug(title string) string {
	title = displayTitle(title)
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "task"
	}
	return s
}
func displayTitle(title string) string {
	title = bracketPrefix.ReplaceAllString(strings.TrimSpace(title), "")
	title = ordinalPrefix.ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}
func ticketFilename(number int, title string) string {
	return fmt.Sprintf("%02d-%s.md", number, titleSlug(title))
}
func ticketNumber(name string) (int, bool) {
	m := ticketNumberRE.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil && n > 0
}

func bodyType(body string) string {
	lines := strings.Split(normalizeLF(body), "\n")
	if len(lines) > 10 {
		lines = lines[:10]
	}
	for _, line := range lines {
		v := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(v, "type:") {
			v = strings.TrimSpace(strings.TrimPrefix(v, "type:"))
			switch v {
			case "research", "prototype", "grilling", "task":
				return v
			}
		}
	}
	return "task"
}
func hasHeading(body, heading string) bool { _, _, ok := section(body, heading); return ok }
func section(body, heading string) (string, string, bool) {
	body = normalizeLF(body)
	lines := strings.Split(body, "\n")
	target := "## " + heading
	start := -1
	end := len(lines)
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			start = i
			continue
		}
		if start >= 0 && i > start && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			end = i
			break
		}
	}
	if start < 0 {
		return "", body, false
	}
	return strings.TrimSpace(strings.Join(lines[start+1:end], "\n")), body, true
}
func mergeSection(body, heading, prose string) string {
	body = strings.TrimRight(normalizeLF(body), "\n")
	lines := strings.Split(body, "\n")
	target := "## " + heading
	start := -1
	end := len(lines)
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			start = i
			continue
		}
		if start >= 0 && i > start && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			end = i
			break
		}
	}
	replacement := []string{target, "", strings.TrimSpace(prose)}
	if start < 0 {
		if body == "" {
			return strings.Join(replacement, "\n")
		}
		return body + "\n\n" + strings.Join(replacement, "\n")
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, replacement...)
	if end < len(lines) {
		out = append(out, "")
		out = append(out, lines[end:]...)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}
func removeSection(body, heading string) string {
	body = normalizeLF(body)
	lines := strings.Split(body, "\n")
	target := "## " + heading
	start := -1
	end := len(lines)
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			start = i
			continue
		}
		if start >= 0 && i > start && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			end = i
			break
		}
	}
	if start < 0 {
		return strings.TrimSpace(body)
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, lines[end:]...)
	return strings.TrimSpace(strings.Join(out, "\n"))
}
func fileBodyForTask(body string) string {
	body = normalizeLF(body)
	if !regexp.MustCompile(`(?m)^## `).MatchString(body) {
		if body == "" {
			return "## Question"
		}
		return "## Question\n\n" + body
	}
	return body
}

type ticketFile struct {
	Number    int
	Path      string
	SG        string
	Type      string
	Blocked   []int
	ClaimedBy string
	ClaimedAt string
	Title     string
	Body      string
	Raw       []byte
}

func parseTicket(data []byte) (ticketFile, error) {
	text := normalizeLF(string(data))
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return ticketFile{}, fmt.Errorf("missing frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return ticketFile{}, fmt.Errorf("unterminated frontmatter")
	}
	t := ticketFile{Raw: []byte(text)}
	for _, line := range lines[1:end] {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "sg":
			t.SG = val
		case "type":
			t.Type = val
		case "claimed_by":
			t.ClaimedBy = val
		case "claimed_at":
			t.ClaimedAt = val
		case "blocked_by":
			val = strings.TrimSpace(val)
			val = strings.TrimPrefix(strings.TrimSuffix(val, "]"), "[")
			if val != "" {
				for _, p := range strings.Split(val, ",") {
					n, err := strconv.Atoi(strings.TrimSpace(p))
					if err == nil && n > 0 {
						t.Blocked = append(t.Blocked, n)
					}
				}
			}
		}
	}
	h1 := -1
	for i := end + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# ") {
			h1 = i
			t.Title = strings.TrimSpace(strings.TrimPrefix(lines[i], "# "))
			break
		}
	}
	if h1 < 0 || t.Title == "" {
		return ticketFile{}, fmt.Errorf("missing H1")
	}
	bodyLines := lines[h1+1:]
	if len(bodyLines) > 0 && bodyLines[0] == "" {
		bodyLines = bodyLines[1:]
	}
	if len(bodyLines) > 0 && bodyLines[len(bodyLines)-1] == "" {
		bodyLines = bodyLines[:len(bodyLines)-1]
	}
	t.Body = strings.Join(bodyLines, "\n")
	return t, nil
}
func renderTicket(task models.Task, number int, blocked []int, foreign, foreignAt string, now time.Time) ([]byte, bool) {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "type: %s\n", bodyType(task.Body))
	b.WriteString("blocked_by: [")
	for i, n := range blocked {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%02d", n)
	}
	b.WriteString("]\n")
	fmt.Fprintf(&b, "sg: %s\n", task.ID)
	if foreign != "" {
		fmt.Fprintf(&b, "claimed_by: %s\n", foreign)
		if foreignAt != "" {
			fmt.Fprintf(&b, "claimed_at: %s\n", foreignAt)
		}
	} else if task.Status == models.StatusInProgress {
		b.WriteString("claimed_by: sg\n")
		fmt.Fprintf(&b, "claimed_at: %s\n", now.UTC().Format(time.RFC3339))
	}
	b.WriteString("---\n\n# ")
	b.WriteString(displayTitle(task.Title))
	b.WriteString("\n\n")
	body := fileBodyForTask(task.Body)
	synth := false
	if task.Status == models.StatusDone && !hasHeading(body, "Answer") {
		body = mergeSection(body, "Answer", "Resolved in Segments (no recorded answer).")
		synth = true
	} else if task.Status == models.StatusClosed && !hasHeading(body, "Ruled out") {
		body = mergeSection(body, "Ruled out", "Closed in Segments.")
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	return []byte(b.String()), synth
}
func taskFieldHash(t models.Task) string {
	return contentHash([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s", t.Title, t.Body, t.Status, t.Priority, strings.Join(t.BlockedBy, "\x00"), t.UpdatedAt.UTC().Format(time.RFC3339Nano))))
}
func mapTask(tasks []models.Task, slug string) *models.Task {
	prefix := "[" + strings.ToLower(slug) + "] map"
	for i := range tasks {
		title := strings.ToLower(tasks[i].Title)
		if title == prefix || strings.HasPrefix(title, prefix+" — ") {
			return &tasks[i]
		}
	}
	return nil
}
func renderMapTask(projectName, slug string, t models.Task) []byte {
	prefix := "[" + slug + "] map — "
	title := t.Title
	if strings.HasPrefix(strings.ToLower(title), strings.ToLower(prefix)) {
		title = title[len(prefix):]
	}
	if strings.EqualFold(title, "["+slug+"] map") || strings.TrimSpace(title) == "" {
		title = projectName
	}
	body := normalizeLF(t.Body)
	text := "# " + strings.TrimSpace(title) + "\n\n" + body
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte(text)
}
func synthesizedMap(projectName string) []byte {
	return []byte(fmt.Sprintf("# %s\n\n## Destination\n\n%s is tracked in Segments. This map is a live view of the Segments\nproject; resolve tickets normally and the bridge records answers back.\n\n## Notes\n\nTickets are synced bidirectionally with Segments by `sg map sync`. Do not\nhand-edit ticket numbering or the `sg` frontmatter key.\n\n## Decisions so far\n\n## Not yet specified\n\n## Out of scope\n", projectName, projectName))
}
func parseMap(data []byte) (string, string, error) {
	text := normalizeLF(string(data))
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			bodyLines := lines[i+1:]
			if len(bodyLines) > 0 && bodyLines[0] == "" {
				bodyLines = bodyLines[1:]
			}
			if len(bodyLines) > 0 && bodyLines[len(bodyLines)-1] == "" {
				bodyLines = bodyLines[:len(bodyLines)-1]
			}
			return strings.TrimSpace(strings.TrimPrefix(line, "# ")), strings.Join(bodyLines, "\n"), nil
		}
	}
	return "", "", fmt.Errorf("map.md missing H1")
}
func firstSentence(answer string) string {
	s := strings.Join(strings.Fields(answer), " ")
	if i := strings.IndexAny(s, ".!?"); i >= 0 {
		s = s[:i+1]
	}
	runes := []rune(s)
	if len(runes) > 120 {
		s = strings.TrimSpace(string(runes[:117])) + "..."
	}
	return s
}
