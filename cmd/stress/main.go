package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	defaultProjectID = "67011e8b-10f5-4ea7-94da-8840e3e0b5d9"
	stressPrefix     = "[Stress] "
	runIDMarker      = "stress-run-id:"
)

type task struct {
	ID        string   `json:"id"`
	ProjectID string   `json:"project_id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Priority  int      `json:"priority"`
	Body      string   `json:"body"`
	BlockedBy []string `json:"blocked_by,omitempty"`
}

// updateReq is the PUT body. Priority has NO omitempty: we always send -1 so the
// store preserves the existing priority. Other fields use omitempty so empty
// values hit the preserve-on-empty path in store.UpdateTask.
type updateReq struct {
	ProjectID string   `json:"project_id"`
	Title     string   `json:"title,omitempty"`
	Body      string   `json:"body,omitempty"`
	Status    string   `json:"status,omitempty"`
	Priority  int      `json:"priority"`
	BlockedBy []string `json:"blocked_by,omitempty"`
}

type client struct {
	host string
	hc   *http.Client
}

func newClient(host string) *client {
	return &client{host: host, hc: &http.Client{Timeout: 10 * time.Second}}
}

func (c *client) url(path string) string {
	return "http://" + c.host + path
}

func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *client) createTask(ctx context.Context, projectID, title, body string, priority int) (*task, error) {
	req := map[string]any{"title": title, "body": body, "priority": priority}
	var out task
	if err := c.do(ctx, "POST", "/api/projects/"+projectID+"/tasks", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) updateTask(ctx context.Context, id string, u updateReq) (*task, error) {
	var out task
	if err := c.do(ctx, "PUT", "/api/tasks/"+id, u, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) listTasks(ctx context.Context, projectID string) ([]task, error) {
	var out []task
	if err := c.do(ctx, "GET", "/api/projects/"+projectID+"/tasks", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) deleteTask(ctx context.Context, projectID, id string) error {
	return c.do(ctx, "DELETE", "/api/tasks/"+id+"?project_id="+projectID, nil, nil)
}

var titlePool = []string{
	"Compile shader cache",
	"Bake lightmaps",
	"Pack texture atlas",
	"Run integration tests",
	"Deploy to staging",
	"Warm CDN",
	"Rotate KMS keys",
	"Rebuild search index",
	"Migrate schema",
	"Smoke-test checkout",
	"Generate sitemap",
	"Cut release notes",
	"Reindex vector store",
	"Snapshot metrics",
	"Vacuum database",
	"Refresh materialized views",
	"Scrub PII from logs",
	"Patch kernel modules",
	"Publish OCI image",
	"Seed demo fixtures",
	"Audit access tokens",
	"Benchmark cold starts",
	"Purge expired sessions",
	"Roll canary deploy",
	"Stitch monorepo bundles",
	"Tag release candidate",
}

var shapes = []string{"chain", "fan-out", "diamond", "tree"}

type dagNode struct {
	title    string
	priority int
	deps     []int
	primary  int
	id       string
}

func buildDAG(shape string) []dagNode {
	mk := func(deps []int, primary int) dagNode {
		return dagNode{
			title:    titlePool[mrand.IntN(len(titlePool))],
			priority: pickPriority(),
			deps:     deps,
			primary:  primary,
		}
	}
	switch shape {
	case "chain":
		return []dagNode{mk(nil, -1), mk([]int{0}, 0), mk([]int{1}, 1), mk([]int{2}, 2)}
	case "fan-out":
		return []dagNode{mk(nil, -1), mk([]int{0}, 0), mk([]int{0}, 0), mk([]int{0}, 0)}
	case "diamond":
		return []dagNode{mk(nil, -1), mk([]int{0}, 0), mk([]int{0}, 0), mk([]int{1, 2}, 1)}
	case "tree":
		return []dagNode{mk(nil, -1), mk([]int{0}, 0), mk([]int{0}, 0), mk([]int{1}, 1), mk([]int{1}, 1), mk([]int{2}, 2), mk([]int{2}, 2)}
	}
	return nil
}

func pickPriority() int {
	r := mrand.IntN(100)
	switch {
	case r < 10:
		return 1
	case r < 70:
		return 2
	default:
		return 3
	}
}

type stats struct {
	live      atomic.Int64
	created   atomic.Int64
	completed atomic.Int64
}

type config struct {
	projectID string
	host      string
	agents    int
	rate      time.Duration
	maxLive   int
}

func newRunID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func shortID(id string) string {
	if len(id) >= 4 {
		return id[:4] + "…"
	}
	return id
}

var logMu sync.Mutex

func logChange(agent int, taskID, shape, title, from, to string) {
	logMu.Lock()
	defer logMu.Unlock()
	fmt.Printf("%s  agent=%d  task=%s  %-7s  %-32s  %s→%s\n",
		time.Now().Format("15:04:05"), agent, shortID(taskID), shape, title, from, to)
}

func jitter(ctx context.Context, minMs, maxMs int) {
	span := maxMs - minMs
	if span < 1 {
		span = 1
	}
	d := time.Duration(minMs+mrand.IntN(span)) * time.Millisecond
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func transition(ctx context.Context, c *client, cfg *config, agentID int, shape string, n *dagNode, from, to string) error {
	u := updateReq{ProjectID: cfg.projectID, Priority: -1, Status: to}
	if _, err := c.updateTask(ctx, n.id, u); err != nil {
		return err
	}
	logChange(agentID, n.id, shape, n.title, from, to)
	return nil
}

func waitForLiveBelow(ctx context.Context, st *stats, maxLive int) bool {
	for int(st.live.Load()) >= maxLive {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return true
}

func runAgent(ctx context.Context, id int, c *client, cfg *config, runID string, st *stats) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("agent=%d panic: %v", id, r)
		}
	}()

	ticker := time.NewTicker(cfg.rate)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}
		if !waitForLiveBelow(ctx, st, cfg.maxLive) {
			return
		}

		shape := shapes[mrand.IntN(len(shapes))]
		nodes := buildDAG(shape)
		body := fmt.Sprintf("%s %s\nshape: %s\nagent: %d", runIDMarker, runID, shape, id)

		abort := false
		for i := range nodes {
			t, err := c.createTask(ctx, cfg.projectID, stressPrefix+nodes[i].title, body, nodes[i].priority)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("agent=%d create failed: %v", id, err)
				abort = true
				break
			}
			nodes[i].id = t.ID
			st.live.Add(1)
			st.created.Add(1)
			logChange(id, t.ID, shape, nodes[i].title, "new", "todo")
			if nodes[i].primary >= 0 {
				parentID := nodes[nodes[i].primary].id
				u := updateReq{ProjectID: cfg.projectID, Priority: -1, BlockedBy: []string{parentID}}
				if _, err := c.updateTask(ctx, t.ID, u); err != nil {
					if ctx.Err() != nil {
						return
					}
					log.Printf("agent=%d set blocked_by failed: %v", id, err)
				}
			}
			if i < len(nodes)-1 {
				jitter(ctx, 500, 1200)
				if ctx.Err() != nil {
					return
				}
			}
		}
		if abort {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			continue
		}

		done := make(map[int]bool, len(nodes))
		for i := range nodes {
			for _, d := range nodes[i].deps {
				for !done[d] {
					select {
					case <-ctx.Done():
						return
					case <-time.After(50 * time.Millisecond):
					}
				}
			}

			if err := transition(ctx, c, cfg, id, shape, &nodes[i], "todo", "in_progress"); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("agent=%d to in_progress failed: %v", id, err)
			}
			jitter(ctx, 800, 2500)
			if ctx.Err() != nil {
				return
			}

			if mrand.IntN(10) == 0 {
				if err := transition(ctx, c, cfg, id, shape, &nodes[i], "in_progress", "blocker"); err != nil {
					if ctx.Err() != nil {
						return
					}
					log.Printf("agent=%d to blocker failed: %v", id, err)
				}
				jitter(ctx, 1500, 3500)
				if ctx.Err() != nil {
					return
				}
				if err := transition(ctx, c, cfg, id, shape, &nodes[i], "blocker", "in_progress"); err != nil {
					if ctx.Err() != nil {
						return
					}
					log.Printf("agent=%d unblock failed: %v", id, err)
				}
				jitter(ctx, 800, 2500)
				if ctx.Err() != nil {
					return
				}
			}

			if err := transition(ctx, c, cfg, id, shape, &nodes[i], "in_progress", "done"); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("agent=%d to done failed: %v", id, err)
			}
			done[i] = true
			st.completed.Add(1)
			st.live.Add(-1)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func cleanup(c *client, projectID, runID string, all bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	allTasks, err := c.listTasks(ctx, projectID)
	if err != nil {
		log.Printf("cleanup: list failed: %v", err)
		return
	}

	var targets []task
	marker := runIDMarker + " " + runID
	for _, t := range allTasks {
		if !strings.HasPrefix(t.Title, stressPrefix) {
			continue
		}
		if !all && !strings.Contains(t.Body, marker) {
			continue
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		fmt.Println("cleanup: no tasks to delete")
		return
	}

	byID := make(map[string]task, len(targets))
	for _, t := range targets {
		byID[t.ID] = t
	}
	depth := make(map[string]int, len(targets))
	var chainLen func(id string, seen map[string]bool) int
	chainLen = func(id string, seen map[string]bool) int {
		if seen[id] {
			return 0
		}
		seen[id] = true
		t, ok := byID[id]
		if !ok || len(t.BlockedBy) == 0 {
			return 0
		}
		best := 0
		for _, bid := range t.BlockedBy {
			if n := chainLen(bid, seen); n > best {
				best = n
			}
		}
		return 1 + best
	}
	for _, t := range targets {
		depth[t.ID] = chainLen(t.ID, map[string]bool{})
	}
	sort.Slice(targets, func(i, j int) bool {
		return depth[targets[i].ID] > depth[targets[j].ID]
	})

	fmt.Printf("cleanup: deleting %d tasks\n", len(targets))
	deleted := 0
	for _, t := range targets {
		if err := c.deleteTask(ctx, projectID, t.ID); err != nil {
			log.Printf("cleanup: delete %s failed: %v", t.ID, err)
			continue
		}
		deleted++
	}
	fmt.Printf("cleanup: deleted %d/%d\n", deleted, len(targets))
}

func summaryLoop(ctx context.Context, st *stats) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			logMu.Lock()
			fmt.Printf("%s  summary  live=%d  created=%d  completed=%d  cleanup-pending=%d\n",
				time.Now().Format("15:04:05"),
				st.live.Load(), st.created.Load(), st.completed.Load(), st.created.Load())
			logMu.Unlock()
		}
	}
}

func main() {
	var cfg config
	var cleanupOnly, keep bool
	flag.StringVar(&cfg.projectID, "project", defaultProjectID, "project UUID to stress")
	flag.IntVar(&cfg.agents, "agents", 1, "number of concurrent agents")
	flag.StringVar(&cfg.host, "host", "127.0.0.1:8765", "segments server host:port")
	flag.DurationVar(&cfg.rate, "rate", 8*time.Second, "minimum delay between new batches per agent")
	flag.IntVar(&cfg.maxLive, "max", 20, "soft cap on live [Stress] tasks before pausing new batches")
	flag.BoolVar(&cleanupOnly, "cleanup-only", false, "delete every [Stress]* task (all runs) and exit")
	flag.BoolVar(&keep, "keep", false, "skip cleanup on exit")
	flag.Parse()

	c := newClient(cfg.host)

	if cleanupOnly {
		cleanup(c, cfg.projectID, "", true)
		return
	}

	if cfg.agents < 1 {
		fmt.Fprintln(os.Stderr, "agents must be >= 1")
		os.Exit(2)
	}

	runID := newRunID()
	fmt.Printf("stress run-id=%s agents=%d host=%s project=%s max=%d rate=%s\n",
		runID, cfg.agents, cfg.host, cfg.projectID, cfg.maxLive, cfg.rate)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var st stats

	defer func() {
		if keep {
			fmt.Println("-keep set, skipping cleanup")
			return
		}
		cleanup(c, cfg.projectID, runID, false)
	}()

	var wg sync.WaitGroup
	for i := 0; i < cfg.agents; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runAgent(ctx, id, c, &cfg, runID, &st)
		}(i)
	}

	sumCtx, sumCancel := context.WithCancel(context.Background())
	go summaryLoop(sumCtx, &st)

	<-ctx.Done()
	fmt.Println("shutting down...")

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		fmt.Println("agent shutdown timeout, proceeding with cleanup")
	}
	sumCancel()
}
