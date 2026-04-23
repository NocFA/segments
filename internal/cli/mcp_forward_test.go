package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"codeberg.org/nocfa/segments/internal/store"
)

// writeFakePidFile redirects the package-level pidFile to dir and writes a
// pid/address line pair pointing forwardMCP at the given httptest server.
// The pid is the current test process so isProcessAlive checks pass.
func writeFakePidFile(t *testing.T, dir, serverURL string) {
	t.Helper()
	origPidFile := pidFile
	pidFile = filepath.Join(dir, "pid")
	t.Cleanup(func() { pidFile = origPidFile })

	addr := strings.TrimPrefix(serverURL, "http://")
	body := strconv.Itoa(os.Getpid()) + "\n" + addr + "\n"
	if err := os.WriteFile(pidFile, []byte(body), 0644); err != nil {
		t.Fatalf("write pid: %v", err)
	}
}

// TestForwardMCP_SendsHeadersAndBody covers the shim-side wire contract in
// isolation: forwardMCP posts to /internal/mcp, sets the CWD / project-id
// / agent headers, and round-trips the daemon's JSON-RPC response.
// Spawning a real `segments mcp` subprocess is flaky on Windows because
// antivirus briefly locks the just-built binary, so we drive the
// forwarder directly against a test server instead.
func TestForwardMCP_SendsHeadersAndBody(t *testing.T) {
	var gotHeaders http.Header
	var gotBody map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/mcp" {
			t.Errorf("path: got %q, want /internal/mcp", r.URL.Path)
		}
		gotHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      gotBody["id"],
			"result":  map[string]interface{}{"echo": gotBody["method"]},
		})
	}))
	defer ts.Close()

	writeFakePidFile(t, t.TempDir(), ts.URL)

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(7),
		"method":  "tools/list",
	}
	resp, err := forwardMCP(ts.Client(), req, "/work/dir", "my-env-id", "claude", "4.7")
	if err != nil {
		t.Fatalf("forwardMCP: %v", err)
	}
	if id, _ := resp["id"].(float64); id != 7 {
		t.Fatalf("response id: got %v, want 7", resp["id"])
	}
	if result, _ := resp["result"].(map[string]interface{}); result["echo"] != "tools/list" {
		t.Fatalf("result: got %v, want echo=tools/list", resp["result"])
	}

	if cwd := gotHeaders.Get(mcpHeaderCWD); cwd != "/work/dir" {
		t.Fatalf("cwd header: got %q, want /work/dir", cwd)
	}
	if env := gotHeaders.Get(mcpHeaderProjectID); env != "my-env-id" {
		t.Fatalf("project-id header: got %q, want my-env-id", env)
	}
	if name := gotHeaders.Get(mcpHeaderAgentName); name != "claude" {
		t.Fatalf("agent-name header: got %q, want claude", name)
	}
	if ver := gotHeaders.Get(mcpHeaderAgentVersion); ver != "4.7" {
		t.Fatalf("agent-version header: got %q, want 4.7", ver)
	}
}

// TestForwardMCP_NotificationReturnsNil asserts the shim maps the daemon's
// 204 response to a nil map, so the shim's main loop knows not to write a
// response line to stdout for a JSON-RPC notification.
func TestForwardMCP_NotificationReturnsNil(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	writeFakePidFile(t, t.TempDir(), ts.URL)

	resp, err := forwardMCP(ts.Client(), map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}, "", "", "", "")
	if err != nil {
		t.Fatalf("forwardMCP: %v", err)
	}
	if resp != nil {
		t.Fatalf("response: got %v, want nil for 204", resp)
	}
}

// TestMCPDaemonHandler_BuildsContextFromHeaders verifies the daemon-side
// wiring: a request with CWD + agent headers reaches handleMCP with an
// mcpContext the tool handlers can use for project auto-resolution and
// analytics. Uses initialize because it exercises the header plumbing
// without needing a populated LMDB store.
func TestMCPDaemonHandler_BuildsContextFromHeaders(t *testing.T) {
	st := store.NewStore(t.TempDir())
	handler := mcpDaemonHandler(st)

	headers := http.Header{}
	headers.Set(mcpHeaderCWD, "/some/cwd")
	headers.Set(mcpHeaderAgentName, "unit-test")
	headers.Set(mcpHeaderAgentVersion, "0.1")

	resp := handler(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"method":  "initialize",
		"params":  map[string]interface{}{"protocolVersion": "2025-06-18"},
	}, headers)

	result, _ := resp["result"].(map[string]interface{})
	if got := result["protocolVersion"]; got != "2025-06-18" {
		t.Fatalf("protocolVersion: got %v, want 2025-06-18", got)
	}
}

// TestForwardMCPGoldenRoundTrip decodes both request and response through
// the same JSON codec the daemon uses, and asserts the bytes out of the
// shim's encoder match the bytes the daemon wrote. Protects against the
// stdio-byte-identical contract regressing if someone swaps the encoder
// or tweaks map ordering later.
func TestForwardMCPGoldenRoundTrip(t *testing.T) {
	want := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result": map[string]interface{}{
			"tools": []interface{}{
				map[string]interface{}{"name": "segments_list_projects"},
			},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	writeFakePidFile(t, t.TempDir(), ts.URL)

	req := map[string]interface{}{"jsonrpc": "2.0", "id": float64(1), "method": "tools/list"}
	resp, err := forwardMCP(ts.Client(), req, "", "", "", "")
	if err != nil {
		t.Fatalf("forwardMCP: %v", err)
	}

	var wantBuf, gotBuf bytes.Buffer
	_ = json.NewEncoder(&wantBuf).Encode(want)
	_ = json.NewEncoder(&gotBuf).Encode(resp)
	if !bytes.Equal(wantBuf.Bytes(), gotBuf.Bytes()) {
		t.Fatalf("stdio bytes drift: want=%q got=%q", wantBuf.String(), gotBuf.String())
	}
}
