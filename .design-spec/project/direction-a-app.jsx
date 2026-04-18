/* Direction A — main app. Dark/light theme, list/kanban/graph. */
const { useState, useEffect, useMemo, useRef, useCallback } = React;

function useTasks(seed) {
  const [tasks, setTasks] = useState(() => seed.tasks.map(t => ({...t})));
  const [pulse, setPulse] = useState({});           // id -> ts
  const [entering, setEntering] = useState({});     // id -> ts

  const update = useCallback((id, patch) => {
    setTasks(ts => ts.map(t => t.id === id ? {...t, ...patch, updated_at: new Date().toISOString()} : t));
    setPulse(p => ({...p, [id]: Date.now()}));
  }, []);

  const add = useCallback((t) => {
    setTasks(ts => [t, ...ts]);
    setEntering(e => ({...e, [t.id]: Date.now()}));
    setTimeout(() => setEntering(e => { const n={...e}; delete n[t.id]; return n; }), 900);
  }, []);

  return { tasks, setTasks, update, add, pulse, entering };
}

// ── Live agent: periodic realistic mutations
function useAgentSim({ tasks, update, add, enabled }) {
  const [log, setLog] = useState([]);
  const pushLog = (msg) => setLog(l => [{ t: Date.now(), msg }, ...l].slice(0,12));

  useEffect(() => {
    if (!enabled) return;
    let i = 0;
    const tick = () => {
      i++;
      const r = Math.random();
      if (r < 0.35) {
        // Flip a todo -> in_progress
        const candidates = tasks.filter(t => t.status === 'todo');
        if (candidates.length) {
          const pick = candidates[Math.floor(Math.random()*candidates.length)];
          update(pick.id, { status: 'in_progress' });
          pushLog(`agent claude-code → ${pick.id} set to in_progress`);
          return;
        }
      }
      if (r < 0.6) {
        const candidates = tasks.filter(t => t.status === 'in_progress');
        if (candidates.length) {
          const pick = candidates[Math.floor(Math.random()*candidates.length)];
          update(pick.id, { status: 'done', closed_at: new Date().toISOString() });
          pushLog(`agent claude-code → ${pick.id} resolved`);
          return;
        }
      }
      if (r < 0.8) {
        // Add a new task
        const titles = [
          'Add graceful degradation when ws disconnects >3s',
          'Agent handoff: include blocked_by graph in /api/ready',
          'Refactor mutation log to CBOR for smaller payloads',
          'Persist panel width + column order in localStorage',
          'Deduplicate flaky CSP report-uri noise from browser extensions',
        ];
        const id = 'SEG-' + (200 + i);
        const newTask = {
          id, project: 'proj_segments',
          title: titles[Math.floor(Math.random()*titles.length)],
          status: 'todo', priority: Math.floor(Math.random()*3)+1,
          body: '(just created by agent)',
          blocked_by: [], sort_order: 999,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          closed_at: null,
        };
        add(newTask);
        pushLog(`agent cursor → created ${id}`);
        return;
      }
      // Otherwise, just pulse a random in_progress task
      const live = tasks.filter(t => t.status === 'in_progress');
      if (live.length) {
        const pick = live[Math.floor(Math.random()*live.length)];
        update(pick.id, {});
        pushLog(`agent claude-code → heartbeat on ${pick.id}`);
      }
    };
    const h = setInterval(tick, 5200);
    return () => clearInterval(h);
  }, [tasks, update, add, enabled]);

  return log;
}

function ATopBar({ tok, theme, setTheme, view, setView, search, setSearch, onPalette, agentOn, setAgentOn }) {
  const tabs = [
    { id: 'list',   label:'List',   hint:'G L' },
    { id: 'kanban', label:'Kanban', hint:'G K' },
    { id: 'graph',  label:'Graph',  hint:'G G' },
  ];
  return (
    <div style={{
      display:'flex', alignItems:'center', gap:14, padding:'10px 16px',
      borderBottom:`1px solid ${tok.line}`, background: tok.bg, position:'sticky', top:0, zIndex:5,
    }}>
      <div style={{ display:'flex', alignItems:'center', gap:8 }}>
        <div style={{
          width:18, height:18, borderRadius:3,
          background:`linear-gradient(135deg, ${tok.accent}, ${tok.text})`,
          position:'relative',
        }}>
          <div style={{ position:'absolute', inset:4, borderRadius:1, background:tok.bg }}/>
        </div>
        <span style={{ color: tok.text, fontSize:13, fontWeight:600, letterSpacing:-0.1 }}>Segments</span>
        <span style={{ color: tok.faint, fontSize:11, fontFamily:'"Geist Mono", monospace' }}>v0.3.1</span>
      </div>

      <div style={{
        display:'flex', gap:2, marginLeft:12, padding:2,
        background: tok.surface, borderRadius:6, border:`1px solid ${tok.line}`,
      }}>
        {tabs.map(t => (
          <button key={t.id} onClick={() => setView(t.id)} style={{
            all:'unset', cursor:'pointer',
            padding:'4px 10px', borderRadius:4, fontSize:12,
            color: view===t.id ? tok.text : tok.muted,
            background: view===t.id ? tok.surface2 : 'transparent',
            fontWeight: view===t.id ? 500 : 400,
            display:'flex', alignItems:'center', gap:6,
          }}>
            {t.label}
            <span style={{ fontSize:10, color:tok.faint, fontFamily:'"Geist Mono", monospace' }}>{t.hint}</span>
          </button>
        ))}
      </div>

      <div style={{ flex:1, display:'flex', justifyContent:'center' }}>
        <div style={{
          display:'flex', alignItems:'center', gap:8,
          background: tok.surface, border:`1px solid ${tok.line}`,
          padding:'5px 10px', borderRadius:6, minWidth:320, maxWidth:460, width:'100%',
        }}>
          <span style={{ color: tok.faint, fontSize:12 }}>⌕</span>
          <input
            value={search}
            onChange={e=>setSearch(e.target.value)}
            placeholder="Search tasks… (press / to focus)"
            data-search-input
            style={{
              all:'unset', flex:1, color: tok.text, fontSize:12.5,
              '--placeholder': tok.faint,
            }}
          />
          <span style={{
            fontSize:10, color:tok.faint, fontFamily:'"Geist Mono", monospace',
            border:`1px solid ${tok.line}`, padding:'1px 4px', borderRadius:3,
          }}>/</span>
        </div>
      </div>

      <button onClick={onPalette} style={{
        all:'unset', cursor:'pointer',
        fontSize:11.5, color:tok.muted,
        padding:'5px 9px', border:`1px solid ${tok.line}`, borderRadius:5,
        fontFamily:'"Geist Mono", monospace',
      }}>⌘K</button>

      <label style={{
        display:'flex', alignItems:'center', gap:6, fontSize:11, color: agentOn ? tok.accent : tok.muted,
        cursor:'pointer', userSelect:'none',
      }}>
        <span className="seg-agent-dot" style={{
          width:6, height:6, borderRadius:3,
          background: agentOn ? tok.accent : tok.faint,
        }}/>
        <span>Agent {agentOn ? 'live' : 'paused'}</span>
        <input type="checkbox" checked={agentOn} onChange={e=>setAgentOn(e.target.checked)} style={{display:'none'}}/>
      </label>

      <button onClick={()=>setTheme(theme==='dark'?'light':'dark')} style={{
        all:'unset', cursor:'pointer', fontSize:14, color:tok.muted, padding:'3px 6px',
      }}>{theme==='dark' ? '☾' : '☀'}</button>
    </div>
  );
}

function ASidebar({ tok, projects, tasksByProject, activeProject, setActiveProject, readyCount }) {
  return (
    <aside style={{
      width:212, borderRight:`1px solid ${tok.line}`, padding:'14px 8px',
      display:'flex', flexDirection:'column', gap:16, background: tok.bg, flexShrink:0,
    }}>
      <div>
        <div style={{ fontSize:10.5, color: tok.faint, letterSpacing:0.6, padding:'0 10px 6px', textTransform:'uppercase' }}>
          Views
        </div>
        <button onClick={()=>setActiveProject(null)} style={{
          all:'unset', cursor:'pointer', width:'100%', boxSizing:'border-box',
          padding:'7px 10px', fontSize:12.5,
          color: !activeProject ? tok.text : tok.muted,
          background: !activeProject ? tok.surface2 : 'transparent', borderRadius:5,
          display:'flex', justifyContent:'space-between',
        }}>
          <span>All tasks</span>
          <span style={{ color: tok.faint, fontSize:10.5 }}>
            {Object.values(tasksByProject).reduce((a,b)=>a+b.length,0)}
          </span>
        </button>
        <button style={{
          all:'unset', cursor:'pointer', width:'100%', boxSizing:'border-box',
          padding:'7px 10px', fontSize:12.5, color: tok.muted, borderRadius:5,
          display:'flex', justifyContent:'space-between', alignItems:'center',
        }}>
          <span>Ready queue</span>
          <span style={{
            color: tok.accent, fontSize:10.5,
            background:tok.accentDim, padding:'1px 5px', borderRadius:3,
          }}>{readyCount}</span>
        </button>
      </div>

      <div>
        <div style={{ fontSize:10.5, color: tok.faint, letterSpacing:0.6, padding:'0 10px 6px', textTransform:'uppercase' }}>
          Projects
        </div>
        <div style={{ display:'flex', flexDirection:'column', gap:1 }}>
          {projects.map(p => (
            <AProjectBadge
              key={p.id} project={p}
              tasks={tasksByProject[p.id] || []}
              tok={tok}
              active={activeProject === p.id}
              onClick={() => setActiveProject(activeProject === p.id ? null : p.id)}
            />
          ))}
        </div>
      </div>

      <div style={{ flex:1 }}/>

      <div style={{
        padding:'10px 12px', fontSize:10.5, color:tok.faint,
        borderTop:`1px solid ${tok.line}`, lineHeight:1.7,
      }}>
        <div style={{ color:tok.muted, marginBottom:4 }}>Shortcuts</div>
        <div><kbd style={kbdStyle(tok)}>j</kbd> <kbd style={kbdStyle(tok)}>k</kbd> nav</div>
        <div><kbd style={kbdStyle(tok)}>c</kbd> create · <kbd style={kbdStyle(tok)}>e</kbd> edit</div>
        <div><kbd style={kbdStyle(tok)}>t</kbd> cycle status</div>
        <div><kbd style={kbdStyle(tok)}>1-4</kbd> priority</div>
        <div><kbd style={kbdStyle(tok)}>g</kbd> then <kbd style={kbdStyle(tok)}>k/l/g</kbd></div>
      </div>
    </aside>
  );
}

function kbdStyle(tok) {
  return {
    fontFamily:'"Geist Mono", monospace', fontSize:9.5,
    border:`1px solid ${tok.line2}`, padding:'0 4px', borderRadius:3,
    color:tok.muted, background:tok.surface,
  };
}

// ── List view
function AListView({ tok, tasks, tasksById, selectedId, setSelectedId, pulse, entering, expandedId, setExpandedId, groupBy }) {
  const groups = useMemo(() => {
    if (groupBy === 'none') return [{ key:'all', label:'All', items: tasks }];
    const order = ['in_progress','blocker','todo','done','closed'];
    const buckets = {};
    order.forEach(s => buckets[s] = []);
    tasks.forEach(t => buckets[t.status].push(t));
    return order.filter(s => buckets[s].length).map(s => ({
      key:s, label:A_STATUS_META[s].label, items: buckets[s],
    }));
  }, [tasks, groupBy]);

  return (
    <div style={{ flex:1, overflow:'auto' }}>
      {groups.map(g => (
        <div key={g.key}>
          <div style={{
            display:'flex', alignItems:'baseline', gap:10,
            padding:'14px 16px 6px',
            fontSize:11, letterSpacing:0.5, textTransform:'uppercase',
            color:tok.muted, background: tok.bg,
            position:'sticky', top:0, zIndex:1,
            borderBottom:`1px solid ${tok.line}`,
          }}>
            <span style={{ color: tok[g.key==='in_progress'?'inprog':g.key] || tok.muted, fontSize:13 }}>
              {A_STATUS_META[g.key]?.glyph}
            </span>
            <span>{g.label}</span>
            <span style={{ color:tok.faint, fontSize:10.5 }}>{g.items.length}</span>
          </div>
          {g.items.map(t => (
            <AListRow
              key={t.id} task={t} tasksById={tasksById} tok={tok}
              selected={selectedId === t.id}
              pulsing={pulse[t.id] && Date.now() - pulse[t.id] < 2000}
              entering={!!entering[t.id]}
              onClick={() => setSelectedId(t.id)}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

// ── Kanban view
function AKanbanView({ tok, tasks, tasksById, selectedId, setSelectedId, update, pulse }) {
  const cols = [
    { key:'todo',        label:'Todo' },
    { key:'in_progress', label:'In progress' },
    { key:'blocker',     label:'Blocker' },
    { key:'done',        label:'Done' },
  ];
  const [dragging, setDragging] = useState(null);
  const [overCol, setOverCol] = useState(null);

  const onDragStart = (e, id) => {
    setDragging(id);
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', id);
  };
  const onDragOver = (e, col) => { e.preventDefault(); setOverCol(col); };
  const onDrop = (e, col) => {
    e.preventDefault();
    const id = e.dataTransfer.getData('text/plain') || dragging;
    if (id) update(id, { status: col });
    setDragging(null); setOverCol(null);
  };

  return (
    <div style={{
      flex:1, display:'grid', gridTemplateColumns:`repeat(${cols.length}, 1fr)`,
      gap:0, overflow:'auto',
    }}>
      {cols.map(c => {
        const items = tasks.filter(t => t.status === c.key);
        const highlight = overCol === c.key;
        return (
          <div
            key={c.key}
            onDragOver={e=>onDragOver(e,c.key)} onDrop={e=>onDrop(e,c.key)}
            onDragLeave={()=>setOverCol(null)}
            style={{
              borderRight: `1px solid ${tok.line}`,
              background: highlight ? tok.accentDim : 'transparent',
              transition:'background 120ms',
              display:'flex', flexDirection:'column',
            }}
          >
            <div style={{
              display:'flex', alignItems:'center', justifyContent:'space-between',
              padding:'12px 14px 10px', borderBottom:`1px solid ${tok.line}`,
              position:'sticky', top:0, background:tok.bg, zIndex:1,
            }}>
              <div style={{ display:'flex', gap:8, alignItems:'center' }}>
                <span style={{ color: tok[c.key==='in_progress'?'inprog':c.key], fontSize:13 }}>{A_STATUS_META[c.key].glyph}</span>
                <span style={{ color: tok.text, fontSize:12, fontWeight:500 }}>{c.label}</span>
                <span style={{ color:tok.faint, fontSize:10.5 }}>{items.length}</span>
              </div>
            </div>
            <div style={{ padding:'8px', display:'flex', flexDirection:'column', gap:6, minHeight:300 }}>
              {items.map(t => (
                <div key={t.id}
                  draggable onDragStart={e=>onDragStart(e,t.id)}
                  onClick={()=>setSelectedId(t.id)}
                  className={pulse[t.id] && Date.now()-pulse[t.id]<2000 ? 'seg-pulse' : ''}
                  style={{
                    padding:'10px 11px',
                    background: tok.surface, border:`1px solid ${tok.line}`,
                    borderRadius:5, cursor:'grab',
                    boxShadow: selectedId===t.id ? tok.glow : 'none',
                    opacity: dragging===t.id ? 0.4 : 1,
                  }}
                >
                  <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center', marginBottom:6 }}>
                    <span style={{
                      fontSize:10.5, color:tok.faint, fontFamily:'"Geist Mono", monospace',
                    }}>{t.id}</span>
                    <APriorityBars p={t.priority} tok={tok}/>
                  </div>
                  <div style={{ fontSize:12.5, color:tok.text, lineHeight:1.35, marginBottom:6 }}>{t.title}</div>
                  <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center', gap:6 }}>
                    <ABlockerChip ids={t.blocked_by} tasksById={tasksById} tok={tok}/>
                    <span style={{ color:tok.faint, fontSize:10.5, fontVariantNumeric:'tabular-nums' }}>
                      {SEGMENTS_UTIL.relTime(t.updated_at)}
                    </span>
                  </div>
                </div>
              ))}
              {items.length === 0 && (
                <div style={{ color:tok.faint, fontSize:11.5, padding:'30px 10px', textAlign:'center' }}>
                  drop here
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ── Graph view (top-down DAG)
function AGraphView({ tok, tasks, tasksById, selectedId, setSelectedId }) {
  // Compute rank = longest path from a root; roots have rank 0.
  const layout = useMemo(() => {
    const byId = tasksById;
    const rank = {};
    const visit = (id, seen=new Set()) => {
      if (rank[id] !== undefined) return rank[id];
      if (seen.has(id)) return 0;
      seen.add(id);
      const t = byId[id];
      if (!t || !t.blocked_by || !t.blocked_by.length) { rank[id] = 0; return 0; }
      const r = 1 + Math.max(0, ...t.blocked_by.filter(b=>byId[b]).map(b => visit(b, seen)));
      rank[id] = r; return r;
    };
    tasks.forEach(t => visit(t.id));
    const rows = {};
    tasks.forEach(t => {
      const r = rank[t.id] || 0;
      rows[r] = rows[r] || [];
      rows[r].push(t);
    });
    const sortedRows = Object.keys(rows).map(Number).sort((a,b)=>a-b);
    const W = 180, H = 64, gapX = 36, gapY = 44;
    const pos = {};
    sortedRows.forEach(r => {
      rows[r].sort((a,b) => a.priority - b.priority || a.id.localeCompare(b.id));
      rows[r].forEach((t, i) => { pos[t.id] = { x: 60 + i*(W+gapX), y: 40 + r*(H+gapY), r }; });
    });
    const width = Math.max(...Object.values(pos).map(p=>p.x+W)) + 60;
    const height = 40 + (sortedRows.length)*(H+gapY);
    return { pos, W, H, width, height };
  }, [tasks, tasksById]);

  const ready = useMemo(() => new Set(SEGMENTS_UTIL.ready(tasks).map(t=>t.id)), [tasks]);

  return (
    <div style={{ flex:1, overflow:'auto', position:'relative', background: tok.bg }}>
      <div style={{
        position:'sticky', top:0, zIndex:2, background:tok.bg,
        display:'flex', gap:16, padding:'10px 16px',
        borderBottom:`1px solid ${tok.line}`, fontSize:11, color:tok.muted,
      }}>
        <span>Dependency DAG · top-down</span>
        <span style={{ color:tok.faint }}>·</span>
        <span><span style={{ color: tok.accent }}>●</span> ready (no unresolved blockers)</span>
        <span style={{ color:tok.faint }}>·</span>
        <span>{ready.size} ready / {tasks.length} total</span>
      </div>
      <svg width={layout.width} height={layout.height} style={{ display:'block' }}>
        <defs>
          <marker id="aarrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="5" markerHeight="5" orient="auto">
            <path d="M0,0 L10,5 L0,10 z" fill={tok.faint}/>
          </marker>
        </defs>
        {tasks.map(t => (t.blocked_by||[]).map(b => {
          const from = layout.pos[b], to = layout.pos[t.id];
          if (!from || !to) return null;
          const x1 = from.x + layout.W/2, y1 = from.y + layout.H;
          const x2 = to.x + layout.W/2, y2 = to.y;
          const midY = (y1 + y2) / 2;
          return (
            <path key={t.id+'-'+b}
              d={`M ${x1} ${y1} C ${x1} ${midY} ${x2} ${midY} ${x2} ${y2}`}
              stroke={tok.line2} strokeWidth={1} fill="none" markerEnd="url(#aarrow)"
            />
          );
        }))}
        {tasks.map(t => {
          const p = layout.pos[t.id]; if (!p) return null;
          const isReady = ready.has(t.id);
          const color = t.status==='in_progress'? tok.inprog : t.status==='done'? tok.done : t.status==='blocker'? tok.blocker : t.status==='closed'? tok.closed : tok.todo;
          const sel = selectedId===t.id;
          return (
            <g key={t.id} transform={`translate(${p.x} ${p.y})`}
               onClick={()=>setSelectedId(t.id)} style={{ cursor:'pointer' }}>
              {isReady && (
                <rect x={-3} y={-3} width={layout.W+6} height={layout.H+6} rx={8}
                  fill="none" stroke={tok.accent} strokeWidth={1} opacity={0.55}/>
              )}
              <rect width={layout.W} height={layout.H} rx={6}
                fill={sel ? tok.surface2 : tok.surface}
                stroke={sel ? tok.accent : tok.line2} strokeWidth={1}/>
              <rect width={3} height={layout.H} rx={1.5} fill={color}/>
              <text x={14} y={20} fill={tok.faint} fontSize={9.5}
                fontFamily='"Geist Mono", monospace'>{t.id} · P{t.priority}</text>
              <foreignObject x={14} y={24} width={layout.W-24} height={layout.H-28}>
                <div xmlns="http://www.w3.org/1999/xhtml" style={{
                  fontSize:11.5, color:tok.text, lineHeight:1.3,
                  display:'-webkit-box', WebkitLineClamp:2, WebkitBoxOrient:'vertical', overflow:'hidden',
                  fontFamily:'Geist, system-ui, sans-serif',
                }}>{t.title}</div>
              </foreignObject>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

// ── Side panel (details)
function ADetailPanel({ tok, task, tasksById, onClose, update }) {
  if (!task) return null;
  const deps = (task.blocked_by||[]).map(id => tasksById[id]).filter(Boolean);
  const dependents = Object.values(tasksById).filter(t => (t.blocked_by||[]).includes(task.id));
  const proj = SEGMENTS_SEED.projects.find(p => p.id === task.project);
  return (
    <aside style={{
      width:380, flexShrink:0, borderLeft:`1px solid ${tok.line}`,
      background: tok.bg, overflow:'auto',
    }}>
      <div style={{ padding:'12px 18px', borderBottom:`1px solid ${tok.line}`,
        display:'flex', justifyContent:'space-between', alignItems:'center',
      }}>
        <span style={{ fontSize:11, color:tok.faint, fontFamily:'"Geist Mono", monospace' }}>
          {task.id} · {proj?.name}
        </span>
        <button onClick={onClose} style={{ all:'unset', cursor:'pointer', color:tok.muted, fontSize:14 }}>✕</button>
      </div>

      <div style={{ padding:'14px 18px 8px' }}>
        <h2 style={{
          margin:0, fontSize:17, fontWeight:500, color: tok.text, lineHeight:1.3,
          letterSpacing:-0.15,
        }}>{task.title}</h2>
      </div>

      <div style={{
        padding:'8px 18px 14px', display:'grid', gridTemplateColumns:'84px 1fr',
        columnGap:12, rowGap:8, fontSize:12, color:tok.muted,
        borderBottom:`1px solid ${tok.line}`,
      }}>
        <span>Status</span>
        <span>
          <select value={task.status} onChange={e=>update(task.id,{status:e.target.value})}
            style={{ all:'unset', color: tok.text, cursor:'pointer' }}>
            {Object.keys(A_STATUS_META).map(s => <option key={s} value={s} style={{background:tok.bg}}>{A_STATUS_META[s].label}</option>)}
          </select>
        </span>
        <span>Priority</span>
        <span style={{ display:'flex', alignItems:'center', gap:8, color: tok.text }}>
          <APriorityBars p={task.priority} tok={tok}/> P{task.priority}
        </span>
        <span>Updated</span>
        <span style={{ color: tok.text, fontFamily:'"Geist Mono", monospace', fontSize:11 }}>
          {SEGMENTS_UTIL.relTime(task.updated_at)} · {new Date(task.updated_at).toISOString().slice(0,16).replace('T',' ')}
        </span>
        <span>Created</span>
        <span style={{ color: tok.text, fontFamily:'"Geist Mono", monospace', fontSize:11 }}>
          {SEGMENTS_UTIL.relTime(task.created_at)}
        </span>
        {task.closed_at && (<>
          <span>Closed</span>
          <span style={{ color: tok.text, fontFamily:'"Geist Mono", monospace', fontSize:11 }}>
            {SEGMENTS_UTIL.relTime(task.closed_at)}
          </span>
        </>)}
      </div>

      <div style={{ padding:'14px 18px', color:tok.text, fontSize:13.5, lineHeight:1.6, whiteSpace:'pre-wrap',
        fontFamily:'Geist, system-ui, sans-serif',
      }}>
        {task.body || <span style={{color:tok.faint}}>No description.</span>}
      </div>

      {(deps.length>0 || dependents.length>0) && (
        <div style={{ padding:'12px 18px', borderTop:`1px solid ${tok.line}` }}>
          {deps.length>0 && (
            <div style={{ marginBottom:12 }}>
              <div style={{ fontSize:10.5, color:tok.faint, textTransform:'uppercase', letterSpacing:0.6, marginBottom:6 }}>
                Blocked by
              </div>
              {deps.map(d => (
                <div key={d.id} style={{ display:'flex', alignItems:'center', gap:8, padding:'4px 0', fontSize:12, color: tok.text }}>
                  <AStatusPill status={d.status} tok={tok}/>
                  <span style={{ color:tok.faint, fontFamily:'"Geist Mono", monospace', fontSize:11 }}>{d.id}</span>
                  <span style={{ color:tok.muted, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{d.title}</span>
                </div>
              ))}
            </div>
          )}
          {dependents.length>0 && (
            <div>
              <div style={{ fontSize:10.5, color:tok.faint, textTransform:'uppercase', letterSpacing:0.6, marginBottom:6 }}>
                Blocking
              </div>
              {dependents.map(d => (
                <div key={d.id} style={{ display:'flex', alignItems:'center', gap:8, padding:'4px 0', fontSize:12, color: tok.text }}>
                  <AStatusPill status={d.status} tok={tok}/>
                  <span style={{ color:tok.faint, fontFamily:'"Geist Mono", monospace', fontSize:11 }}>{d.id}</span>
                  <span style={{ color:tok.muted, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{d.title}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </aside>
  );
}

// ── Command palette
function APalette({ tok, open, onClose, tasks, onJump, setView, theme, setTheme, update, selectedId }) {
  const [q, setQ] = useState('');
  const [idx, setIdx] = useState(0);
  const inputRef = useRef(null);

  useEffect(() => { if (open) { setQ(''); setIdx(0); setTimeout(()=>inputRef.current?.focus(),10); } }, [open]);

  const items = useMemo(() => {
    const base = [
      { type:'action', label:'Switch to List view',   run:()=>setView('list') },
      { type:'action', label:'Switch to Kanban view', run:()=>setView('kanban') },
      { type:'action', label:'Switch to Graph view',  run:()=>setView('graph') },
      { type:'action', label:`Theme: ${theme==='dark'?'Light':'Dark'} mode`, run:()=>setTheme(theme==='dark'?'light':'dark') },
      ...(selectedId ? [
        { type:'action', label:`Mark ${selectedId} done`, run:()=>update(selectedId,{status:'done', closed_at:new Date().toISOString()}) },
        { type:'action', label:`Mark ${selectedId} in_progress`, run:()=>update(selectedId,{status:'in_progress'}) },
      ]:[]),
      ...tasks.slice(0,40).map(t => ({ type:'task', label: t.title, sub: t.id, run:()=>onJump(t.id) })),
    ];
    if (!q) return base;
    const nq = q.toLowerCase();
    return base.filter(it => it.label.toLowerCase().includes(nq) || (it.sub||'').toLowerCase().includes(nq));
  }, [q, tasks, theme, selectedId]);

  if (!open) return null;

  const onKey = (e) => {
    if (e.key==='Escape') onClose();
    else if (e.key==='ArrowDown') { setIdx(i => Math.min(items.length-1, i+1)); e.preventDefault(); }
    else if (e.key==='ArrowUp')   { setIdx(i => Math.max(0, i-1)); e.preventDefault(); }
    else if (e.key==='Enter')     { items[idx]?.run(); onClose(); e.preventDefault(); }
  };

  return (
    <div onClick={onClose} style={{
      position:'absolute', inset:0, background:'rgba(0,0,0,0.4)', zIndex:100,
      display:'flex', justifyContent:'center', alignItems:'flex-start', paddingTop:90,
    }}>
      <div onClick={e=>e.stopPropagation()} style={{
        width:560, background:tok.bg, border:`1px solid ${tok.line2}`, borderRadius:8,
        boxShadow:'0 20px 60px rgba(0,0,0,0.5)', overflow:'hidden',
      }}>
        <input
          ref={inputRef} value={q} onChange={e=>{setQ(e.target.value); setIdx(0);}}
          onKeyDown={onKey}
          placeholder="Type a command or search tasks…"
          style={{
            all:'unset', width:'100%', boxSizing:'border-box',
            padding:'14px 18px', fontSize:14, color:tok.text,
            borderBottom:`1px solid ${tok.line}`,
          }}
        />
        <div style={{ maxHeight:360, overflow:'auto' }}>
          {items.slice(0,30).map((it, i) => (
            <div key={i} onMouseEnter={()=>setIdx(i)} onClick={()=>{it.run(); onClose();}}
              style={{
                padding:'9px 18px', fontSize:12.5,
                background: idx===i ? tok.surface2 : 'transparent',
                color: tok.text, display:'flex', justifyContent:'space-between', cursor:'pointer',
                borderLeft: `2px solid ${idx===i ? tok.accent : 'transparent'}`,
              }}>
              <span>
                <span style={{ color: tok.faint, fontSize:10.5, marginRight:8, fontFamily:'"Geist Mono", monospace' }}>
                  {it.type}
                </span>
                {it.label}
              </span>
              {it.sub && <span style={{ color: tok.faint, fontFamily:'"Geist Mono", monospace', fontSize:11 }}>{it.sub}</span>}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

// ── Composer (inline create)
function AComposer({ tok, open, onClose, onCreate, tasks }) {
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [priority, setPriority] = useState(1);
  const [blocker, setBlocker] = useState('');
  const inputRef = useRef(null);
  useEffect(()=>{ if(open){ setTitle(''); setBody(''); setPriority(1); setBlocker(''); setTimeout(()=>inputRef.current?.focus(),10);}}, [open]);
  if (!open) return null;
  const submit = () => {
    if (!title.trim()) return;
    const id = 'SEG-' + (500 + Math.floor(Math.random()*500));
    onCreate({
      id, project:'proj_segments', title: title.trim(), body: body.trim(),
      priority, status:'todo',
      blocked_by: blocker ? [blocker] : [],
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      sort_order: 0, closed_at: null,
    });
    onClose();
  };
  return (
    <div style={{
      borderBottom:`1px solid ${tok.line}`, background: tok.surface, padding:'12px 16px',
    }}>
      <input ref={inputRef} value={title} onChange={e=>setTitle(e.target.value)}
        onKeyDown={e=>{ if(e.key==='Enter' && !e.shiftKey) { submit(); } if(e.key==='Escape') onClose();}}
        placeholder="Task title — press Enter to create"
        style={{ all:'unset', width:'100%', fontSize:14, color:tok.text, marginBottom:8 }}/>
      <textarea value={body} onChange={e=>setBody(e.target.value)}
        placeholder="Description (optional)"
        rows={2}
        style={{
          width:'100%', boxSizing:'border-box', resize:'none',
          background:'transparent', border:'none', outline:'none',
          color:tok.muted, fontSize:12.5, fontFamily:'inherit',
        }}/>
      <div style={{ display:'flex', gap:12, alignItems:'center', marginTop:4 }}>
        <span style={{ fontSize:11, color:tok.faint }}>Priority</span>
        {[0,1,2,3].map(p => (
          <button key={p} onClick={()=>setPriority(p)} style={{
            all:'unset', cursor:'pointer', padding:'2px 7px', borderRadius:3, fontSize:11,
            border:`1px solid ${priority===p ? tok.accent : tok.line}`,
            color: priority===p ? tok.accent : tok.muted,
          }}>P{p}</button>
        ))}
        <span style={{ fontSize:11, color:tok.faint, marginLeft:8 }}>Blocker</span>
        <input value={blocker} onChange={e=>setBlocker(e.target.value)} placeholder="SEG-###"
          style={{ all:'unset', fontSize:11, color:tok.muted, fontFamily:'"Geist Mono", monospace',
            border:`1px solid ${tok.line}`, padding:'2px 6px', borderRadius:3, width:80 }}/>
        <div style={{ flex:1 }}/>
        <span style={{ fontSize:10.5, color:tok.faint }}>↵ create · esc cancel</span>
      </div>
    </div>
  );
}

// ── Main
function SegmentsA() {
  const [theme, setTheme] = useState('dark');
  const tok = A_TOK[theme];
  const [view, setView] = useState('list');
  const [search, setSearch] = useState('');
  const [selectedId, setSelectedId] = useState('SEG-101');
  const [expandedId, setExpandedId] = useState(null);
  const [activeProject, setActiveProject] = useState(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [composerOpen, setComposerOpen] = useState(false);
  const [agentOn, setAgentOn] = useState(true);

  const { tasks, update, add, pulse, entering } = useTasks(SEGMENTS_SEED);
  useAgentSim({ tasks, update, add, enabled: agentOn });

  const tasksById = useMemo(()=>Object.fromEntries(tasks.map(t=>[t.id,t])), [tasks]);
  const tasksByProject = useMemo(()=>{
    const m = {};
    SEGMENTS_SEED.projects.forEach(p=>m[p.id]=[]);
    tasks.forEach(t => { (m[t.project]=m[t.project]||[]).push(t); });
    return m;
  }, [tasks]);

  const filteredTasks = useMemo(() => {
    let out = tasks;
    if (activeProject) out = out.filter(t => t.project === activeProject);
    if (search) out = SEGMENTS_UTIL.fuzzy(search, out);
    return out;
  }, [tasks, activeProject, search]);

  const readyCount = useMemo(()=>SEGMENTS_UTIL.ready(tasks).length, [tasks]);

  // Keyboard
  useEffect(() => {
    let gPending = false, gTimeout;
    const handler = (e) => {
      const target = e.target;
      const isInput = target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable);

      if (e.key === 'Escape') {
        if (paletteOpen) setPaletteOpen(false);
        else if (composerOpen) setComposerOpen(false);
        else if (selectedId) setSelectedId(null);
        return;
      }

      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault(); setPaletteOpen(p=>!p); return;
      }
      if (isInput) return;

      if (e.key === '/') {
        e.preventDefault();
        const el = document.querySelector('[data-search-input]');
        if (el) el.focus();
        return;
      }
      if (e.key === 'c') { e.preventDefault(); setComposerOpen(true); return; }

      if (gPending) {
        clearTimeout(gTimeout); gPending = false;
        if (e.key === 'k') setView('kanban');
        else if (e.key === 'l') setView('list');
        else if (e.key === 'g') setView('graph');
        return;
      }
      if (e.key === 'g') {
        gPending = true; gTimeout = setTimeout(()=>gPending=false, 800); return;
      }

      // j/k nav
      if (e.key === 'j' || e.key === 'k') {
        const idx = filteredTasks.findIndex(t => t.id === selectedId);
        const next = e.key === 'j' ? Math.min(filteredTasks.length-1, idx+1) : Math.max(0, idx-1);
        if (filteredTasks[next]) setSelectedId(filteredTasks[next].id);
        e.preventDefault(); return;
      }
      if (['1','2','3','4'].includes(e.key) && selectedId) {
        update(selectedId, { priority: parseInt(e.key)-1 });
        return;
      }
      if (e.key === 't' && selectedId) {
        const order = ['todo','in_progress','blocker','done','closed'];
        const cur = tasksById[selectedId]?.status;
        const next = order[(order.indexOf(cur)+1) % order.length];
        update(selectedId, { status: next });
        return;
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [paletteOpen, composerOpen, selectedId, filteredTasks, tasksById, update]);

  return (
    <div style={{
      height:'100vh', width:'100vw', background: tok.bg, color: tok.text,
      fontFamily:'Geist, "Geist Sans", -apple-system, system-ui, sans-serif',
      fontFeatureSettings: '"ss01", "cv11"',
      display:'flex', flexDirection:'column', overflow:'hidden',
    }}>
      <ATopBar tok={tok} theme={theme} setTheme={setTheme} view={view} setView={setView}
        search={search} setSearch={setSearch}
        onPalette={()=>setPaletteOpen(true)} agentOn={agentOn} setAgentOn={setAgentOn}/>
      <div style={{ display:'flex', flex:1, minHeight:0 }}>
        <ASidebar tok={tok} projects={SEGMENTS_SEED.projects}
          tasksByProject={tasksByProject} activeProject={activeProject}
          setActiveProject={setActiveProject} readyCount={readyCount}/>
        <div style={{ flex:1, display:'flex', flexDirection:'column', minWidth:0, position:'relative' }}>
          <AComposer tok={tok} open={composerOpen} onClose={()=>setComposerOpen(false)} onCreate={add} tasks={tasks}/>
          {view==='list' && (
            <AListView tok={tok} tasks={filteredTasks} tasksById={tasksById}
              selectedId={selectedId} setSelectedId={setSelectedId}
              pulse={pulse} entering={entering} expandedId={expandedId} setExpandedId={setExpandedId}
              groupBy="status"/>
          )}
          {view==='kanban' && (
            <AKanbanView tok={tok} tasks={filteredTasks} tasksById={tasksById}
              selectedId={selectedId} setSelectedId={setSelectedId} update={update} pulse={pulse}/>
          )}
          {view==='graph' && (
            <AGraphView tok={tok} tasks={filteredTasks} tasksById={tasksById}
              selectedId={selectedId} setSelectedId={setSelectedId}/>
          )}
        </div>
        {selectedId && tasksById[selectedId] && (
          <ADetailPanel tok={tok} task={tasksById[selectedId]} tasksById={tasksById}
            onClose={()=>setSelectedId(null)} update={update}/>
        )}
      </div>
      <APalette tok={tok} open={paletteOpen} onClose={()=>setPaletteOpen(false)}
        tasks={tasks} onJump={setSelectedId} setView={setView}
        theme={theme} setTheme={setTheme} update={update} selectedId={selectedId}/>
    </div>
  );
}

window.SegmentsA = SegmentsA;
