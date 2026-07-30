package mapbridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codeberg.org/nocfa/segments/internal/models"
	"codeberg.org/nocfa/segments/internal/store"
)

type Config struct {
	Store   *store.Store
	Project models.Project
	Repo    string
	Out     io.Writer
	Now     func() time.Time
}
type pendingFile struct {
	hash  string
	since time.Time
}
type Bridge struct {
	store                               *store.Store
	project                             models.Project
	repo, mapDir, ticketsDir, statePath string
	out                                 io.Writer
	now                                 func() time.Time
	state                               bridgeState
	pending                             map[string]pendingFile
}

func New(cfg Config) (*Bridge, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("map bridge requires a store")
	}
	if cfg.Repo == "" {
		cfg.Repo = "."
	}
	repo, err := filepath.Abs(cfg.Repo)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(repo)
	if err != nil {
		return nil, fmt.Errorf("repo %s: %w", repo, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repo %s is not a directory", repo)
	}
	slug := projectSlug(cfg.Project.Name)
	if slug == "" {
		return nil, fmt.Errorf("project name %q has no path-safe characters", cfg.Project.Name)
	}
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	dir := filepath.Join(repo, ".plan", "maps", slug)
	sp := statePath(cfg.Store.BasePath(), cfg.Project.ID)
	st, err := loadState(sp, repo)
	if err != nil {
		return nil, fmt.Errorf("load bridge state: %w", err)
	}
	return &Bridge{store: cfg.Store, project: cfg.Project, repo: repo, mapDir: dir, ticketsDir: filepath.Join(dir, "tickets"), statePath: sp, out: cfg.Out, now: cfg.Now, state: st, pending: map[string]pendingFile{}}, nil
}
func (b *Bridge) logf(format string, args ...any) { fmt.Fprintf(b.out, format+"\n", args...) }
func (b *Bridge) Export() error                   { return b.materialize() }
func (b *Bridge) SyncOnce() error {
	if err := b.syncFiles(false); err != nil {
		return err
	}
	return b.materialize()
}
func (b *Bridge) Sync(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if err := b.SyncOnce(); err != nil {
		b.logf("initial map sync warning: %v", err)
	}
	cadence := time.Second
	if interval < cadence {
		cadence = interval
	}
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()
	lastSegments := b.now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			filesOK := true
			if err := b.syncFiles(true); err != nil {
				b.logf("map file sync warning: %v", err)
				filesOK = false
			}
			if filesOK && b.now().Sub(lastSegments) >= interval {
				if err := b.materialize(); err != nil {
					b.logf("segments sync warning: %v", err)
				}
				lastSegments = b.now()
			}
		}
	}
}

func (b *Bridge) tasks() ([]models.Task, *models.Task, error) {
	tasks, err := b.store.ListTasks(b.project.ID)
	if err != nil {
		return nil, nil, err
	}
	mt := mapTask(tasks, projectSlug(b.project.Name))
	return tasks, mt, nil
}
func (b *Bridge) scanTickets() ([]ticketFile, error) {
	entries, err := os.ReadDir(b.ticketsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var files []ticketFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		path := filepath.Join(b.ticketsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		t, err := parseTicket(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		t.Path = path
		t.Number, _ = ticketNumber(e.Name())
		files = append(files, t)
	}
	return files, nil
}
func taskMap(tasks []models.Task) map[string]*models.Task {
	m := make(map[string]*models.Task, len(tasks))
	for i := range tasks {
		m[tasks[i].ID] = &tasks[i]
	}
	return m
}
func (b *Bridge) reconcile(tasks []models.Task, mt *models.Task, files []ticketFile) {
	known := taskMap(tasks)
	used := map[int]string{}
	for _, f := range files {
		if f.Number <= 0 || f.SG == "" {
			continue
		}
		if _, ok := known[f.SG]; !ok {
			continue
		}
		if mt != nil && f.SG == mt.ID {
			continue
		}
		if owner, ok := used[f.Number]; ok && owner != f.SG {
			continue
		}
		for id, n := range b.state.Numbers {
			if n == f.Number && id != f.SG {
				delete(b.state.Numbers, id)
			}
		}
		b.state.Numbers[f.SG] = f.Number
		used[f.Number] = f.SG
	}
	ids := make([]string, 0, len(tasks))
	for i := range tasks {
		if mt == nil || tasks[i].ID != mt.ID {
			ids = append(ids, tasks[i].ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, c := known[ids[i]], known[ids[j]]
		if !a.CreatedAt.Equal(c.CreatedAt) {
			return a.CreatedAt.Before(c.CreatedAt)
		}
		// Bulk MCP creates share a timestamp; SortOrder keeps the batch order.
		if a.SortOrder != c.SortOrder {
			return a.SortOrder < c.SortOrder
		}
		return a.ID < c.ID
	})
	for _, id := range ids {
		if n := b.state.Numbers[id]; n > 0 {
			if owner, collision := used[n]; !collision || owner == id {
				used[n] = id
				continue
			}
			delete(b.state.Numbers, id)
		}
		n := b.nextFree(used)
		b.state.Numbers[id] = n
		used[n] = id
	}
}
func (b *Bridge) nextFree(used map[int]string) int {
	for n := 1; ; n++ {
		if _, ok := used[n]; ok {
			continue
		}
		if containsNumber(b.state.Retired, n) {
			continue
		}
		return n
	}
}
func (b *Bridge) numberToID() map[int]string {
	m := map[int]string{}
	for id, n := range b.state.Numbers {
		m[n] = id
	}
	return m
}
func sameStrings(a, c []string) bool {
	if len(a) != len(c) {
		return false
	}
	for i := range a {
		if a[i] != c[i] {
			return false
		}
	}
	return true
}

func (b *Bridge) syncFiles(debounce bool) error {
	tasks, mt, err := b.tasks()
	if err != nil {
		return err
	}
	files, err := b.scanTickets()
	if err != nil {
		return err
	}
	b.reconcile(tasks, mt, files)
	known := taskMap(tasks)
	used := map[int]string{}
	for id, n := range b.state.Numbers {
		used[n] = id
	}
	// Import new or unknown-sg tickets before applying changes to known tickets.
	for i := range files {
		f := &files[i]
		if f.SG != "" {
			if _, ok := known[f.SG]; ok {
				continue
			}
		}
		n := f.Number
		if n <= 0 || used[n] != "" {
			n = b.nextFree(used)
		}
		body := normalizeLF(f.Body)
		task, err := b.store.CreateTask(b.project.ID, f.Title, body, 2)
		if err != nil {
			return fmt.Errorf("import %s: %w", f.Path, err)
		}
		b.state.Numbers[task.ID] = n
		used[n] = task.ID
		known[task.ID] = task
		f.SG = task.ID
		f.Number = n
		blocked := b.translateFileBlockers(f.Blocked, b.numberToID())
		if len(blocked) > 0 {
			task, err = b.store.UpdateTask(b.project.ID, task.ID, store.TaskPatch{BlockedBy: &blocked})
			if err != nil {
				return err
			}
			known[task.ID] = task
		}
		if err := b.applyTicket(*task, *f); err != nil {
			return err
		}
		target := filepath.Join(b.ticketsDir, ticketFilename(n, task.Title))
		if !samePath(f.Path, target) {
			if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		b.logf("segments task created -> tickets/%s", ticketFilename(n, task.Title))
	}
	tasks, mt, err = b.tasks()
	if err != nil {
		return err
	}
	known = taskMap(tasks)
	files, err = b.scanTickets()
	if err != nil {
		return err
	}
	b.reconcile(tasks, mt, files)
	for _, f := range files {
		task := known[f.SG]
		if task == nil {
			continue
		}
		key := stateFileKey(f.Number)
		hash := contentHash(f.Raw)
		fs := b.state.Files[key]
		if hash == fs.WrittenHash || hash == fs.SeenHash {
			delete(b.pending, f.Path)
			continue
		}
		if debounce {
			p, ok := b.pending[f.Path]
			if !ok || p.hash != hash {
				b.pending[f.Path] = pendingFile{hash: hash, since: b.now()}
				continue
			}
			if b.now().Sub(p.since) < 300*time.Millisecond {
				continue
			}
		}
		if err := b.applyTicket(*task, f); err != nil {
			b.logf("warning: sync %s: %v", f.Path, err)
			continue
		}
		fs.SeenHash = hash
		b.state.Files[key] = fs
		delete(b.pending, f.Path)
	}
	if mt == nil && b.state.MapTaskHash == "" {
		mapPath := filepath.Join(b.mapDir, "map.md")
		if data, err := os.ReadFile(mapPath); err == nil && !b.state.Synthesized.MapMD {
			title, body, err := parseMap(data)
			if err == nil {
				task, err := b.store.CreateTask(b.project.ID, "["+projectSlug(b.project.Name)+"] map — "+title, body, 2)
				if err != nil {
					return err
				}
				status := models.StatusInProgress
				if _, err = b.store.UpdateTask(b.project.ID, task.ID, store.TaskPatch{Status: &status}); err != nil {
					return err
				}
				b.logf("map task created -> %s", task.ID)
			}
		}
	}
	return saveState(b.statePath, b.state)
}
func (b *Bridge) translateFileBlockers(numbers []int, lookup map[int]string) []string {
	out := []string{}
	for _, n := range numbers {
		if id := lookup[n]; id != "" {
			out = append(out, id)
		} else {
			b.logf("warning: unknown blocked_by ticket %02d dropped", n)
		}
	}
	return out
}
func (b *Bridge) applyTicket(task models.Task, f ticketFile) error {
	patch := store.TaskPatch{}
	changed := false
	candidate := normalizeLF(f.Body)
	answer, _, hasAnswer := section(candidate, "Answer")
	ruled, _, hasRuled := section(candidate, "Ruled out")
	synth := containsNumber(b.state.Synthesized.Answer, f.Number) && answer == "Resolved in Segments (no recorded answer)."
	synthRuled := ruled == "Closed in Segments." && !hasHeading(task.Body, "Ruled out")
	targetBody := candidate
	if synth {
		targetBody = removeSection(targetBody, "Answer")
	}
	if synthRuled {
		targetBody = removeSection(targetBody, "Ruled out")
		hasRuled = false
	}
	if strings.TrimSpace(targetBody) != strings.TrimSpace(fileBodyForTask(task.Body)) {
		patch.Body = &targetBody
		changed = true
	}
	var targetStatus *models.TaskStatus
	if hasAnswer && answer != "" && !synth && task.Status != models.StatusDone {
		v := models.StatusDone
		targetStatus = &v
		if patch.Body == nil {
			body := mergeSection(task.Body, "Answer", answer)
			patch.Body = &body
		}
		b.logf("%02d answered -> done", f.Number)
		b.appendDecision(task.Title, f.Number, answer)
	} else if hasRuled && ruled != "" && task.Status != models.StatusClosed {
		v := models.StatusClosed
		targetStatus = &v
		if patch.Body == nil {
			body := mergeSection(task.Body, "Ruled out", ruled)
			patch.Body = &body
		}
		b.logf("%02d ruled out -> closed", f.Number)
	} else if f.ClaimedBy != "" && f.ClaimedBy != "sg" && task.Status != models.StatusInProgress {
		v := models.StatusInProgress
		targetStatus = &v
		b.state.Claims[stateFileKey(f.Number)] = claimState{Foreign: f.ClaimedBy, Was: string(task.Status)}
		b.logf("%02d claimed by chartr (%s) -> in_progress", f.Number, f.ClaimedBy)
	} else if f.ClaimedBy == "" && task.Status == models.StatusInProgress {
		if _, ok := b.state.Claims[stateFileKey(f.Number)]; ok {
			v := models.StatusTodo
			targetStatus = &v
			delete(b.state.Claims, stateFileKey(f.Number))
			b.logf("%02d claim released -> todo", f.Number)
		}
	}
	if targetStatus != nil {
		patch.Status = targetStatus
		changed = true
	}
	cross := []string{}
	for _, id := range task.BlockedBy {
		if b.state.Numbers[id] == 0 {
			cross = append(cross, id)
		}
	}
	same := b.translateFileBlockers(f.Blocked, b.numberToID())
	merged := append(cross, same...)
	if !sameStrings(task.BlockedBy, merged) {
		patch.BlockedBy = &merged
		changed = true
	}
	if changed {
		_, err := b.store.UpdateTask(b.project.ID, task.ID, patch)
		return err
	}
	return nil
}

func (b *Bridge) materialize() error {
	if err := os.MkdirAll(b.ticketsDir, 0755); err != nil {
		return err
	}
	tasks, mt, err := b.tasks()
	if err != nil {
		return err
	}
	files, err := b.scanTickets()
	if err != nil {
		return err
	}
	b.reconcile(tasks, mt, files)
	known := taskMap(tasks)
	paths := map[string]ticketFile{}
	for _, f := range files {
		if f.SG != "" {
			paths[f.SG] = f
		}
	}
	// Retire and delete tasks no longer present in Segments.
	for id, n := range b.state.Numbers {
		if known[id] != nil && (mt == nil || id != mt.ID) {
			continue
		}
		if old, ok := paths[id]; ok {
			if err := os.Remove(old.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			b.logf("segments task deleted -> removed tickets/%s", filepath.Base(old.Path))
		}
		b.state.Retired = appendNumber(b.state.Retired, n)
		delete(b.state.Numbers, id)
		delete(b.state.Files, stateFileKey(n))
		delete(b.state.Claims, stateFileKey(n))
	}
	sort.Slice(tasks, func(i, j int) bool { return b.state.Numbers[tasks[i].ID] < b.state.Numbers[tasks[j].ID] })
	for _, task := range tasks {
		if mt != nil && task.ID == mt.ID {
			continue
		}
		n := b.state.Numbers[task.ID]
		blocked := []int{}
		hidden := 0
		for _, id := range task.BlockedBy {
			if bn := b.state.Numbers[id]; bn > 0 {
				blocked = append(blocked, bn)
			} else {
				hidden++
			}
		}
		sort.Ints(blocked)
		if hidden > 0 && b.state.Files[stateFileKey(n)].TaskHash != taskFieldHash(task) {
			b.logf("%02d warning: %d cross-project blocker(s) hidden", n, hidden)
		}
		claim, at := "", ""
		if old, ok := paths[task.ID]; ok && old.ClaimedBy != "" {
			if old.ClaimedBy != "sg" || task.Status == models.StatusInProgress {
				claim, at = old.ClaimedBy, old.ClaimedAt
			}
		}
		data, synth := renderTicket(task, n, blocked, claim, at, b.now())
		target := filepath.Join(b.ticketsDir, ticketFilename(n, task.Title))
		old, exists := paths[task.ID]
		if exists {
			if _, pending := b.pending[old.Path]; pending {
				continue
			}
		}
		current, readErr := os.ReadFile(target)
		needsWrite := readErr != nil || contentHash(current) != contentHash(data)
		if needsWrite {
			if err := atomicWrite(target, data, 0644); err != nil {
				return err
			}
			if !exists || errors.Is(readErr, os.ErrNotExist) {
				b.logf("%02d materialized -> tickets/%s", n, filepath.Base(target))
			} else {
				b.logf("%02d updated -> tickets/%s", n, filepath.Base(target))
			}
		}
		if exists && !samePath(old.Path, target) {
			if err := os.Remove(old.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			b.logf("%02d title changed -> tickets/%s", n, filepath.Base(target))
		}
		hash := contentHash(data)
		b.state.Files[stateFileKey(n)] = fileState{WrittenHash: hash, SeenHash: hash, TaskHash: taskFieldHash(task)}
		if synth {
			b.state.Synthesized.Answer = appendNumber(b.state.Synthesized.Answer, n)
		} else {
			b.state.Synthesized.Answer = removeNumber(b.state.Synthesized.Answer, n)
		}
	}
	if err := b.materializeMap(mt); err != nil {
		return err
	}
	return saveState(b.statePath, b.state)
}
func samePath(a, c string) bool {
	aa, _ := filepath.Abs(a)
	cc, _ := filepath.Abs(c)
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(cc))
}
func removeNumber(ns []int, n int) []int {
	out := ns[:0]
	for _, v := range ns {
		if v != n {
			out = append(out, v)
		}
	}
	return out
}
func (b *Bridge) materializeMap(mt *models.Task) error {
	path := filepath.Join(b.mapDir, "map.md")
	if mt != nil {
		hash := taskFieldHash(*mt)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) || hash != b.state.MapTaskHash {
			if err := atomicWrite(path, renderMapTask(b.project.Name, projectSlug(b.project.Name), *mt), 0644); err != nil {
				return err
			}
			b.logf("map task updated -> map.md")
		}
		b.state.MapTaskHash = hash
		b.state.Synthesized.MapMD = false
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := atomicWrite(path, synthesizedMap(b.project.Name), 0644); err != nil {
			return err
		}
		b.logf("project map created -> map.md")
		b.state.Synthesized.MapMD = true
	}
	return nil
}
func (b *Bridge) appendDecision(title string, n int, answer string) {
	path := filepath.Join(b.mapDir, "map.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	line := fmt.Sprintf("- [%s](tickets/%s) — %s", displayTitle(title), ticketFilename(n, title), firstSentence(answer))
	text := normalizeLF(string(data))
	if strings.Contains(text, line) {
		return
	}
	heading := "## Decisions so far"
	i := strings.Index(text, heading)
	if i < 0 {
		return
	}
	insert := i + len(heading)
	rest := text[insert:]
	next := strings.Index(rest, "\n## ")
	if next < 0 {
		next = len(rest)
	}
	sectionText := strings.TrimRight(rest[:next], "\n")
	replacement := "\n\n" + line + "\n"
	if strings.TrimSpace(sectionText) != "" {
		replacement = "\n" + sectionText + "\n" + line + "\n"
	}
	updated := text[:insert] + replacement + rest[next:]
	if err := atomicWrite(path, []byte(updated), 0644); err == nil {
		b.logf("%02d decision appended -> map.md", n)
	}
}
