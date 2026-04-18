// Shared seed state for all three Segments prototypes.
// Single-operator. Tasks form a DAG via blocked_by.
// Dates anchored to 2026-04-17 (today).

window.SEGMENTS_SEED = (() => {
  const NOW = new Date('2026-04-17T14:30:00Z').getTime();
  const ago = (mins) => new Date(NOW - mins * 60_000).toISOString();

  const projects = [
    { id: 'proj_hooks',    name: 'hooks.server',   slug: 'hooks',   color: 'steel'  },
    { id: 'proj_psx',      name: 'psx-renderer',   slug: 'psx',     color: 'amber'  },
    { id: 'proj_segments', name: 'segments-core',  slug: 'core',    color: 'green'  },
    { id: 'proj_infra',    name: 'infra',          slug: 'infra',   color: 'violet' },
  ];

  const t = (id, o) => ({
    id, project: o.project,
    title: o.title,
    status: o.status,           // todo | in_progress | done | closed | blocker
    priority: o.priority,       // 0 (urgent) .. 3 (trivial)
    body: o.body || '',
    blocked_by: o.blocked_by || [],
    created_at: ago(o.c),
    updated_at: ago(o.u),
    closed_at:  o.cl ? ago(o.cl) : null,
    sort_order: o.s,
  });

  const tasks = [
    t('SEG-101', { project:'proj_hooks', title:'Configure CSP + security headers in hooks.server.ts',
      status:'in_progress', priority:0, c:2880, u:42, s:1,
      body:`Add a global handle() that sets Content-Security-Policy, Strict-Transport-Security, Referrer-Policy, Permissions-Policy and X-Content-Type-Options.

CSP should allow self, inline for dev only, and the websocket origin. Produce a nonce per-request and thread it through +layout.svelte so inline scripts can opt in.

Acceptance:
- observatory.mozilla.org scores A+ on /
- no CSP violations in devtools on any route
- HSTS preload-ready (includeSubDomains; preload)` }),

    t('SEG-102', { project:'proj_hooks', title:'Thread request-id through handle() and structured logs',
      status:'todo', priority:1, c:2700, u:600, s:2, blocked_by:['SEG-101'],
      body:`Every request gets a ULID. Store on event.locals, echo as x-request-id header, attach to every pino log line from the handler. Downstream fetches to worker propagate it.` }),

    t('SEG-103', { project:'proj_hooks', title:'Rate-limit /api/ingest by IP + token',
      status:'blocker', priority:0, c:1600, u:120, s:3,
      body:`Leaky bucket, 30 rpm per-token, 200 rpm per-IP. Shared Redis. Fails open on Redis outage but emits a loud log.

BLOCKED: waiting on SEG-310 to provision staging redis.`,
      blocked_by:['SEG-310'] }),

    t('SEG-104', { project:'proj_segments', title:'Strengthen create_tasks schema description to prevent stringified-array bug',
      status:'done', priority:1, c:4320, u:220, cl:220, s:4,
      body:`Claude kept passing blocked_by as a JSON string instead of an array. Expanded the zod .describe() to explicitly show array shape and added a refine() that rejects strings with a helpful error.` }),

    t('SEG-105', { project:'proj_segments', title:'WebSocket: broadcast task mutations to all open tabs',
      status:'in_progress', priority:0, c:1440, u:8, s:5,
      body:`Single ws endpoint /live. On PUT/POST/DELETE to /api/tasks/*, fan out the updated record. Client reconciles by id + updated_at.` }),

    t('SEG-106', { project:'proj_segments', title:'Optimistic update + rollback on ws reject',
      status:'todo', priority:1, c:1380, u:1380, s:6, blocked_by:['SEG-105'],
      body:`Mutations apply locally, then await ws ack. On reject, rewind and toast.` }),

    t('SEG-107', { project:'proj_segments', title:'Command palette: fuzzy match across title + body + id',
      status:'todo', priority:2, c:900, u:900, s:7,
      body:`cmd/ctrl+k. Implement with a trigram index built lazily on first open. Weight title 3x, id 2x, body 1x. Debounce 40ms.` }),

    t('SEG-108', { project:'proj_segments', title:'Graph view: top-down DAG with "ready" highlight ring',
      status:'todo', priority:1, c:780, u:780, s:8, blocked_by:['SEG-105'],
      body:`Nodes = tasks. Edges = blocked_by. Rank by longest path. Ready = todo && all blockers done — render a soft glow.` }),

    t('SEG-109', { project:'proj_segments', title:'Kanban: drag-drop between status columns fires PUT',
      status:'in_progress', priority:1, c:720, u:90, s:9,
      body:`HTML5 DnD. Placeholder ghost on dragover. Optimistic column swap; reconcile with ws broadcast.` }),

    t('SEG-110', { project:'proj_psx', title:'Add PSX shader effects to Three.js scene',
      status:'in_progress', priority:2, c:2160, u:240, s:10,
      body:`Vertex-snap to 320x240 grid in vertex shader, affine-texture pass (no perspective divide on UVs), 5-bit color quantize in fragment, then dither. Gate behind prefers-reduced-motion.` }),

    t('SEG-111', { project:'proj_psx', title:'Texture atlasing for low-poly meshes',
      status:'todo', priority:2, c:1800, u:1800, s:11, blocked_by:['SEG-110'],
      body:`Single 512x512 atlas. UV-pack via MaxRects. Validate mipmap off.` }),

    t('SEG-112', { project:'proj_psx', title:'Swap MeshStandardMaterial -> custom ShaderMaterial',
      status:'done', priority:2, c:3200, u:2600, cl:2600, s:12,
      body:`Keeps vertex colors, ditches PBR. ~40% fps uplift on integrated GPUs.` }),

    t('SEG-113', { project:'proj_psx', title:'First-person controller with fixed 15fps animation tick',
      status:'todo', priority:3, c:1200, u:1200, s:13,
      body:`Step/pose update at 15Hz, interpolate nothing. Feels chunkier, more authentic.` }),

    t('SEG-114', { project:'proj_segments', title:'Keyboard-only nav: j/k, enter, c, e, /, g+k/l/g',
      status:'in_progress', priority:0, c:660, u:12, s:14,
      body:`Single global listener. Ignore when an input is focused unless esc. Document in ?-overlay.` }),

    t('SEG-115', { project:'proj_segments', title:'Inline composer: title+enter creates instantly',
      status:'done', priority:1, c:1080, u:900, cl:900, s:15,
      body:`Press 'c' to reveal. Tab into body / priority / blocked_by. Enter with only title is valid.` }),

    t('SEG-116', { project:'proj_segments', title:'Client-side full-text search over title + body',
      status:'todo', priority:2, c:540, u:540, s:16, blocked_by:['SEG-107'],
      body:`Reuse the trigram index from palette. '/' focuses the search bar; esc exits.` }),

    t('SEG-117', { project:'proj_infra', title:'Nightly snapshot of sqlite db to R2',
      status:'done', priority:1, c:6000, u:4200, cl:4200, s:17,
      body:`litestream, 30 day retention. Verified restore weekly.` }),

    t('SEG-118', { project:'proj_infra', title:'Migrate from better-sqlite3 to libsql for WAL + http',
      status:'todo', priority:2, c:3600, u:3600, s:18,
      body:`Keep the same schema. Benchmark write throughput under 10 concurrent agent writes.` }),

    t('SEG-119', { project:'proj_infra', title:'Structured audit log of every agent-initiated mutation',
      status:'todo', priority:0, c:2400, u:400, s:19, blocked_by:['SEG-102'],
      body:`Immutable append-only table. actor=agent_name, action, before, after, request_id.` }),

    t('SEG-120', { project:'proj_segments', title:'Priority auto-bump: P3 tasks untouched >14d -> P2',
      status:'closed', priority:3, c:9000, u:8000, cl:8000, s:20,
      body:`Wont fix — noisy. Keep priority stable; surface stale via a filter instead.` }),

    t('SEG-121', { project:'proj_segments', title:'"Ready queue" view: todo tasks with zero unresolved blockers',
      status:'todo', priority:1, c:360, u:360, s:21,
      body:`This is what the agent should pull from next. Sort by priority, then oldest first.` }),

    t('SEG-310', { project:'proj_infra', title:'Provision staging Redis for rate-limit + session store',
      status:'in_progress', priority:0, c:1700, u:180, s:22,
      body:`Upstash, eu-west-1. Terraform module lives in infra/redis/.` }),
  ];

  return { projects, tasks, now: NOW };
})();

// Small helpers shared across prototypes.
window.SEGMENTS_UTIL = {
  relTime(iso, now = Date.now()) {
    const diff = Math.max(0, (now - new Date(iso).getTime()) / 1000);
    if (diff < 45) return 'just now';
    if (diff < 90) return '1m ago';
    if (diff < 3600) return Math.round(diff / 60) + 'm ago';
    if (diff < 5400) return '1h ago';
    if (diff < 86400) return Math.round(diff / 3600) + 'h ago';
    if (diff < 86400 * 2) return 'yesterday';
    if (diff < 86400 * 14) return Math.round(diff / 86400) + 'd ago';
    return Math.round(diff / 86400 / 7) + 'w ago';
  },
  firstLine(body) {
    if (!body) return '';
    return body.split('\n').find(l => l.trim()) || '';
  },
  fuzzy(q, tasks) {
    if (!q) return tasks;
    const needle = q.toLowerCase();
    return tasks.filter(t =>
      t.id.toLowerCase().includes(needle) ||
      t.title.toLowerCase().includes(needle) ||
      (t.body || '').toLowerCase().includes(needle)
    );
  },
  counts(tasks) {
    const c = { todo:0, in_progress:0, blocker:0, done:0, closed:0 };
    tasks.forEach(t => c[t.status]++);
    return c;
  },
  ready(tasks) {
    const byId = Object.fromEntries(tasks.map(t => [t.id, t]));
    return tasks.filter(t =>
      t.status === 'todo' &&
      (t.blocked_by || []).every(b => byId[b] && byId[b].status === 'done')
    );
  },
};
