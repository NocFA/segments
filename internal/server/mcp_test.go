package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"codeberg.org/nocfa/segments/internal/store"
)

// TestHandleMCP_ForwardsToRegisteredHandler checks that POST /internal/mcp
// decodes the body, calls the handler with the headers, and serializes the
// response back. The handler asserts on the headers it receives so we cover
// the CWD + project-id + agent forwarding wire contract in one test.
func TestHandleMCP_ForwardsToRegisteredHandler(t *testing.T) {
	srv := NewServer(&store.Store{}, NewHub(), &Config{}, "")

	var gotHeaders http.Header
	var gotReq map[string]interface{}
	srv.SetMCPHandler(func(req map[string]interface{}, headers http.Header) map[string]interface{} {
		gotReq = req
		gotHeaders = headers.Clone()
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  map[string]interface{}{"ok": true},
		}
	})

	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"method":  "tools/list",
	})
	r := httptest.NewRequest("POST", "/internal/mcp", bytes.NewReader(body))
	r.Header.Set("X-Segments-Cwd", "/tmp/work")
	r.Header.Set("X-Segments-Project-Id", "abc")
	r.Header.Set("X-Segments-Agent-Name", "claude")
	r.Header.Set("X-Segments-Agent-Version", "4.7")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if method, _ := gotReq["method"].(string); method != "tools/list" {
		t.Fatalf("handler got method %q, want tools/list", method)
	}
	if cwd := gotHeaders.Get("X-Segments-Cwd"); cwd != "/tmp/work" {
		t.Fatalf("cwd header: got %q, want /tmp/work", cwd)
	}
	if env := gotHeaders.Get("X-Segments-Project-Id"); env != "abc" {
		t.Fatalf("project-id header: got %q, want abc", env)
	}
	if name := gotHeaders.Get("X-Segments-Agent-Name"); name != "claude" {
		t.Fatalf("agent-name header: got %q, want claude", name)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	result, _ := resp["result"].(map[string]interface{})
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("result.ok: got %v, want true (resp: %s)", result, w.Body.String())
	}
}

// TestHandleMCP_NotificationReturns204 covers the JSON-RPC notification
// path: a request with no id must be answered with 204 No Content so the
// shim does not write anything to stdout.
func TestHandleMCP_NotificationReturns204(t *testing.T) {
	srv := NewServer(&store.Store{}, NewHub(), &Config{}, "")
	srv.SetMCPHandler(func(req map[string]interface{}, _ http.Header) map[string]interface{} {
		return nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	r := httptest.NewRequest("POST", "/internal/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204 (body: %s)", w.Code, w.Body.String())
	}
	if n := w.Body.Len(); n != 0 {
		t.Fatalf("body len: got %d, want 0", n)
	}
}

// TestHandleMCP_NoHandlerReturns503 guards against silent routing drops:
// if the daemon somehow starts without a registered handler, POSTs should
// fail loudly rather than hang or return stale data.
func TestHandleMCP_NoHandlerReturns503(t *testing.T) {
	srv := NewServer(&store.Store{}, NewHub(), &Config{}, "")

	body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	r := httptest.NewRequest("POST", "/internal/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	b, _ := io.ReadAll(w.Body)
	if !bytes.Contains(b, []byte("mcp handler not registered")) {
		t.Fatalf("body: %s", b)
	}
}
