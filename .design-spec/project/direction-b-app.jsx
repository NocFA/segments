/* Direction B — "Console"
   Monospace, amber/green phosphor, info-dense, typewriter feel.
   No scanlines — just careful type + a lot of ASCII-influenced detail.
*/
const { useState: useStateB, useEffect: useEffectB, useMemo: useMemoB, useRef: useRefB, useCallback: useCallbackB } = React;

const B_TOK = {
  dark: {
    bg:       '#0a0b08',
    surface:  '#10110d',
    surface2: '#17190f',
    line:     'rgba(195,165,90,0.12)',
    line2:    'rgba(195,165,90,0.25)',
    text:     '#d7c89a',
    dim:      '#8b7d4c',
    faint:    '#57502e',
    amber:    '#e0b84a',
    green:    '#8fc66b',
    cyan:     '#6bc6c6',
    red:      '#e06a4a',
    todo:     '#8b7d4c',
    inprog:   '#e0b84a',
    done:     '#8fc66b',
    blocker:  '#e06a4a',
    closed:   '#3e3a24',
  },
  light: {
    bg:       '#f6f1e3',
    surface:  '#fffaee',
    surface2: '#f0e8d1',
    line:     'rgba(90,70,10,0.15)',
    line2:    'rgba(90,70,10,0.28)',
    text:     '#2a2410',
    dim:      '#6a5c30',
    faint:    '#9a8c5c',
    amber:    '#9a6b10',
    green:    '#3f6a1a',
    cyan:     '#2a6a6a',
    red:      '#a83a1a',
    todo:     '#6a5c30',
    inprog:   '#9a6b10',
    done:     '#3f6a1a',
    blocker:  '#a83a1a',
    closed:   '#b0a880',
  }
};

// Status as bracketed ASCII
const B_STATUS_META = {
  todo:        { label:'TODO', mark:'[ ]' },
  in_progress: { label:'WORK', mark:'[~]' },
  blocker:     { label:'BLOK', mark:'[!]' },
  done:        { label:'DONE', mark:'[x]' },
  closed:      { label:'WONT', mark:'[/]' },
};

function BPri({ p, tok }) {
  const bangs = ['!!!','!!','!','·'];
  const colors = [tok.red, tok.amber, tok.cyan, tok.faint];
  return <span style={{
    color: colors[p], fontFamily:'"JetBrains Mono", monospace',
    fontWeight:600, fontSize:11, letterSpacing:-0.5, minWidth:24, display:'inline-block',
  }}>{bangs[p]}</span>;
}

function BPill({ status, tok, pulsing }) {
  const meta = B_STATUS_META[status];
  const color = tok[status === 'in_progress' ? 'inprog' : status];
  return (
    <span className={pulsing?'seg-pulse-b':''} style={{
      color, fontFamily:'"JetBrains Mono", monospace',
      fontSize:11, fontWeight:500, letterSpacing:0.5,
    }}>
      <span style={{ color: color, marginRight:4 }}>{meta.mark}</span>
      {meta.label}
    </span>
  );
}

function BBlockerChip({ ids, tasksById, tok }) {
  if (!ids || !ids.length) return null;
  const unresolved = ids.some(id => tasksById[id] && tasksById[id].status !== 'done');
  return (
    <span style={{
      color: unresolved ? tok.red : tok.green,
      fontFamily:'"JetBrains Mono", monospace', fontSize:10.5,
    }}>
      {unresolved ? '⊘' : '✓'}{ids.join(',')}
    </span>
  );
}

function BRow({ task, tasksById, tok, selected, onClick, pulsing, entering }) {
  const body1 = SEGMENTS_UTIL.firstLine(task.body);
  return (
    <div role="row" onClick={onClick}
      className={entering ? 'seg-enter-b' : ''}
      style={{
        display:'grid',
        gridTemplateColumns:'14px 30px 82px 78px 1fr auto auto',
        gap:10, alignItems:'center',
        padding:'4px 14px',
        background: selected ? tok.surface2 : 'transparent',
        cursor:'pointer', position:'relative',
        fontFamily:'"JetBrains Mono", monospace', fontSize:12,
        borderLeft:`2px solid ${selected ? tok.amber : 'transparent'}`,
      }}
    >
      <span style={{ color: selected ? tok.amber : tok.faint }}>{selected ? '▸' : ' '}</span>
      <BPri p={task.priority} tok={tok}/>
      <BPill status={task.status} tok={tok} pulsing={pulsing}/>
      <span style={{ color: tok.dim, fontSize:10.5 }}>{task.id}</span>
      <span style={{ minWidth:0, display:'flex', gap:10, alignItems:'baseline', overflow:'hidden' }}>
        <span style={{
          color: task.status==='closed'? tok.faint : tok.text,
          whiteSpace:'nowrap', overflow:'hidden', textOverflow:'ellipsis',
          flexShrink:1, minWidth:0, fontFamily:'"IBM Plex Mono", "JetBrains Mono", monospace',
        }}>{task.title}</span>
        {body1 && (
          <span style={{
            color: tok.dim, fontSize:11,
            whiteSpace:'nowrap', overflow:'hidden', textOverflow:'ellipsis', flex:1, minWidth:0,
          }}>// {body1}</span>
        )}
      </span>
      <BBlockerChip ids={task.blocked_by} tasksById={tasksById} tok={tok}/>
      <span style={{ color: tok.faint, fontSize:10.5, minWidth:40, textAlign:'right' }}>
        {SEGMENTS_UTIL.relTime(task.updated_at)}
      </span>
    </div>
  );
}

function BTop({ tok, theme, setTheme, view, setView, search, setSearch, onPalette, agentOn, setAgentOn, counts }) {
  return (
    <div style={{
      padding:'0 14px', borderBottom:`1px solid ${tok.line}`,
      background: tok.bg, fontFamily:'"JetBrains Mono", monospace',
    }}>
      <div style={{ display:'flex', alignItems:'center', gap:14, padding:'10px 0 8px' }}>
        <div style={{ display:'flex', alignItems:'baseline', gap:8 }}>
          <span style={{ color: tok.amber, fontSize:14, fontWeight:600 }}>segments</span>
          <span style={{ color: tok.dim, fontSize:11 }}>~/dev/segments $</span>
          <span className="seg-blink" style={{ color: tok.amber }}>_</span>
        </div>

        <div style={{ display:'flex', gap:4, marginLeft:12 }}>
          {[['list','L'],['kanban','K'],['graph','G']].map(([v,k]) => (
            <button key={v} onClick={()=>setView(v)} style={{
              all:'unset', cursor:'pointer',
              padding:'3px 8px', fontSize:11,
              color: view===v ? tok.bg : tok.dim,
              background: view===v ? tok.amber : 'transparent',
              border: `1px solid ${view===v ? tok.amber : tok.line}`,
            }}>
              {v.toUpperCase()} <span style={{opacity:0.6}}>g{k.toLowerCase()}</span>
            </button>
          ))}
        </div>

        <div style={{ flex:1 }}/>

        <div style={{
          display:'flex', alignItems:'center', gap:6,
          border: `1px solid ${tok.line}`, padding:'3px 10px',
          background: tok.surface, minWidth:280,
        }}>
          <span style={{ color: tok.dim, fontSize:11 }}>grep:</span>
          <input value={search} onChange={e=>setSearch(e.target.value)} data-search-input
            placeholder="title body id"
            style={{ all:'unset', flex:1, color:tok.text, fontSize:11.5, fontFamily:'inherit' }}/>
        </div>

        <button onClick={onPalette} style={{
          all:'unset', cursor:'pointer', fontSize:10.5, color:tok.dim,
          padding:'3px 7px', border:`1px solid ${tok.line}`,
        }}>⌘k</button>

        <label style={{ display:'flex', alignItems:'center', gap:5, fontSize:10.5, color: tok.dim, cursor:'pointer' }}>
          <span style={{
            width:6, height:6, borderRadius:0, background: agentOn ? tok.green : tok.faint,
          }} className={agentOn?'seg-blink':''}/>
          AGENT {agentOn?'ONLINE':'OFF'}
          <input type="checkbox" checked={agentOn} onChange={e=>setAgentOn(e.target.checked)} style={{display:'none'}}/>
        </label>

        <button onClick={()=>setTheme(theme==='dark'?'light':'dark')} style={{
          all:'unset', cursor:'pointer', fontSize:11, color:tok.dim, fontFamily:'inherit',
        }}>{theme==='dark'?'[DARK]':'[LITE]'}</button>
      </div>

      <div style={{
        display:'flex', gap:18, padding:'4px 0 8px', fontSize:10.5, color:tok.dim,
        borderTop:`1px dashed ${tok.line}`,
      }}>
        <span>tasks: <span style={{color:tok.text}}>{counts.total}</span></span>
        <span style={{color:tok.inprog}}>{B_STATUS_META.in_progress.mark} {counts.in_progress}</span>
        <span style={{color:tok.blocker}}>{B_STATUS_META.blocker.mark} {counts.blocker}</span>
        <span style={{color:tok.todo}}>{B_STATUS_META.todo.mark} {counts.todo}</span>
        <span style={{color:tok.done}}>{B_STATUS_META.done.mark} {counts.done}</span>
        <span style={{color:tok.closed}}>{B_STATUS_META.closed.mark} {counts.closed}</span>
        <span style={{flex:1}}/>
        <span>ready: <span style={{color:tok.green}}>{counts.ready}</span></span>
        <span>last write: <span style={{color:tok.amber}}>{counts.lastWrite}</span></span>
      </div>
    </div>
  );
}

function BSide({ tok, projects, tasksByProject, activeProject, setActiveProject, agentLog }) {
  return (
    <aside style={{
      width:220, borderRight:`1px solid ${tok.line}`, background: tok.bg,
      display:'flex', flexDirection:'column', flexShrink:0,
      fontFamily:'"JetBrains Mono", monospace', fontSize:11,
    }}>
      <div style={{ padding:'12px 14px 8px', color:tok.dim, fontSize:10, letterSpacing:0.6 }}>
        -- PROJECTS --
      </div>
      <div>
        <button onClick={()=>setActiveProject(null)} style={{
          all:'unset', cursor:'pointer', width:'100%', boxSizing:'border-box',
          padding:'4px 14px', display:'flex', justifyContent:'space-between',
          background: !activeProject ? tok.surface2 : 'transparent',
          color: !activeProject ? tok.amber : tok.text,
        }}>
          <span>{!activeProject?'▸ ':'  '}*</span>
          <span style={{color: tok.dim}}>{Object.values(tasksByProject).reduce((a,b)=>a+b.length,0)}</span>
        </button>
        {projects.map(p => {
          const tasks = tasksByProject[p.id] || [];
          const c = SEGMENTS_UTIL.counts(tasks);
          const active = activeProject === p.id;
          return (
            <button key={p.id} onClick={()=>setActiveProject(active?null:p.id)} style={{
              all:'unset', cursor:'pointer', width:'100%', boxSizing:'border-box',
              padding:'4px 14px', display:'flex', gap:6, alignItems:'center',
              background: active ? tok.surface2 : 'transparent',
              color: active ? tok.amber : tok.text,
            }}>
              <span style={{ width:10 }}>{active?'▸':' '}</span>
              <span style={{ flex:1, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{p.name}</span>
              <span style={{ color: tok.inprog }}>{c.in_progress||''}</span>
              <span style={{ color: tok.blocker }}>{c.blocker||''}</span>
              <span style={{ color: tok.dim, minWidth:18, textAlign:'right' }}>{tasks.length}</span>
            </button>
          );
        })}
      </div>

      <div style={{ padding:'14px 14px 6px', color:tok.dim, fontSize:10, letterSpacing:0.6 }}>
        -- AGENT TAIL --
      </div>
      <div style={{
        flex:1, overflow:'auto', padding:'2px 14px 14px', fontSize:10,
        color: tok.dim, lineHeight:1.5,
      }}>
        {agentLog.length===0 && <div style={{color:tok.faint}}>$ waiting for events…</div>}
        {agentLog.map((l,i) => (
          <div key={i} style={{ marginBottom:4, opacity: Math.max(0.2, 1 - i*0.08) }}>
            <span style={{color:tok.faint}}>{new Date(l.t).toISOString().slice(11,19)}</span>{' '}
            <span style={{color:tok.text}}>{l.msg}</span>
          </div>
        ))}
      </div>

      <div style={{ borderTop:`1px solid ${tok.line}`, padding:'10px 14px', color:tok.faint, fontSize:10, lineHeight:1.6 }}>
        <span style={{color:tok.dim}}>keys:</span> j/k · c · e · / · gl/gk/gg<br/>
        1-4 pri · t cycle · esc close<br/>
        ⌘k palette
      </div>
    </aside>
  );
}

function BListView({ tok, tasks, tasksById, selectedId, setSelectedId, pulse, entering, groupBy='status' }) {
  const groups = useMemoB(() => {
    if (groupBy==='none') return [{key:'all', items:tasks}];
    const order = ['in_progress','blocker','todo','done','closed'];
    const buckets = {}; order.forEach(s=>buckets[s]=[]);
    tasks.forEach(t=>buckets[t.status].push(t));
    return order.filter(s=>buckets[s].length).map(s=>({key:s, items:buckets[s]}));
  }, [tasks, groupBy]);

  return (
    <div style={{ flex:1, overflow:'auto', fontFamily:'"JetBrains Mono", monospace' }}>
      {groups.map(g => (
        <div key={g.key}>
          <div style={{
            padding:'10px 14px 4px', fontSize:10.5,
            color: tok[g.key==='in_progress'?'inprog':g.key] || tok.dim,
            letterSpacing:0.5, borderBottom:`1px dashed ${tok.line}`,
            position:'sticky', top:0, background:tok.bg, zIndex:1,
          }}>
            ── {B_STATUS_META[g.key]?.label} · {g.items.length} ─────────────────────
          </div>
          {g.items.map(t => (
            <BRow key={t.id} task={t} tasksById={tasksById} tok={tok}
              selected={selectedId===t.id}
              pulsing={pulse[t.id] && Date.now()-pulse[t.id]<2000}
              entering={!!entering[t.id]}
              onClick={()=>setSelectedId(t.id)}/>
          ))}
        </div>
      ))}
    </div>
  );
}

function BKanban({ tok, tasks, tasksById, selectedId, setSelectedId, update, pulse }) {
  const cols = ['todo','in_progress','blocker','done'];
  const [dragging, setDragging] = useStateB(null);
  const [overCol, setOverCol] = useStateB(null);

  return (
    <div style={{
      flex:1, display:'grid', gridTemplateColumns:`repeat(${cols.length}, 1fr)`,
      overflow:'auto', fontFamily:'"JetBrains Mono", monospace',
    }}>
      {cols.map(c => {
        const items = tasks.filter(t => t.status===c);
        const color = tok[c==='in_progress'?'inprog':c];
        return (
          <div key={c}
            onDragOver={e=>{e.preventDefault(); setOverCol(c);}}
            onDragLeave={()=>setOverCol(null)}
            onDrop={e=>{
              e.preventDefault();
              const id = e.dataTransfer.getData('text/plain');
              if (id) update(id, {status:c});
              setDragging(null); setOverCol(null);
            }}
            style={{
              borderRight:`1px solid ${tok.line}`,
              background: overCol===c ? 'rgba(224,184,74,0.06)' : 'transparent',
              transition:'background 120ms',
              display:'flex', flexDirection:'column', minHeight:0,
            }}>
            <div style={{
              padding:'10px 12px', borderBottom:`1px solid ${tok.line}`,
              fontSize:11, color, letterSpacing:0.5,
              display:'flex', justifyContent:'space-between', background: tok.bg,
              position:'sticky', top:0, zIndex:1,
            }}>
              <span>{B_STATUS_META[c].mark} {B_STATUS_META[c].label}</span>
              <span style={{color:tok.dim}}>[{items.length}]</span>
            </div>
            <div style={{ padding:8, display:'flex', flexDirection:'column', gap:6 }}>
              {items.map(t => (
                <div key={t.id} draggable
                  onDragStart={e=>{setDragging(t.id); e.dataTransfer.setData('text/plain', t.id);}}
                  onClick={()=>setSelectedId(t.id)}
                  className={pulse[t.id] && Date.now()-pulse[t.id]<2000 ? 'seg-pulse-b':''}
                  style={{
                    padding:'8px 10px', background: tok.surface,
                    border:`1px solid ${selectedId===t.id ? tok.amber : tok.line}`,
                    cursor:'grab', fontSize:11.5, color:tok.text,
                    opacity: dragging===t.id ? 0.35 : 1,
                  }}>
                  <div style={{ display:'flex', justifyContent:'space-between', marginBottom:5, fontSize:10 }}>
                    <span style={{color:tok.dim}}>{t.id}</span>
                    <BPri p={t.priority} tok={tok}/>
                  </div>
                  <div style={{ lineHeight:1.4, marginBottom:6, fontFamily:'"IBM Plex Mono", monospace' }}>{t.title}</div>
                  <div style={{ display:'flex', justifyContent:'space-between', fontSize:10 }}>
                    <BBlockerChip ids={t.blocked_by} tasksById={tasksById} tok={tok}/>
                    <span style={{color:tok.faint}}>{SEGMENTS_UTIL.relTime(t.updated_at)}</span>
                  </div>
                </div>
              ))}
              {items.length===0 && (
                <div style={{ color: tok.faint, fontSize:10.5, padding:20, textAlign:'center' }}>
                  $ drop here
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function BGraph({ tok, tasks, tasksById, selectedId, setSelectedId }) {
  const layout = useMemoB(() => {
    const rank = {};
    const visit = (id, seen=new Set()) => {
      if (rank[id] !== undefined) return rank[id];
      if (seen.has(id)) return 0;
      seen.add(id);
      const t = tasksById[id];
      if (!t || !t.blocked_by || !t.blocked_by.length) { rank[id]=0; return 0; }
      const r = 1 + Math.max(0, ...t.blocked_by.filter(b=>tasksById[b]).map(b=>visit(b, seen)));
      rank[id] = r; return r;
    };
    tasks.forEach(t=>visit(t.id));
    const rows={};
    tasks.forEach(t => { const r = rank[t.id]||0; (rows[r]=rows[r]||[]).push(t); });
    const keys = Object.keys(rows).map(Number).sort((a,b)=>a-b);
    const W=196, H=66, gapX=30, gapY=42;
    const pos={};
    keys.forEach(r => {
      rows[r].sort((a,b)=>a.priority-b.priority);
      rows[r].forEach((t,i)=>{ pos[t.id]={x:40+i*(W+gapX), y:40+r*(H+gapY)}; });
    });
    const width=Math.max(...Object.values(pos).map(p=>p.x+W))+40;
    const height=40+keys.length*(H+gapY);
    return {pos,W,H,width,height};
  }, [tasks, tasksById]);
  const ready = useMemoB(()=>new Set(SEGMENTS_UTIL.ready(tasks).map(t=>t.id)), [tasks]);

  return (
    <div style={{ flex:1, overflow:'auto', background: tok.bg, fontFamily:'"JetBrains Mono", monospace' }}>
      <div style={{
        position:'sticky', top:0, zIndex:2, padding:'10px 14px',
        borderBottom:`1px dashed ${tok.line}`, background:tok.bg,
        fontSize:10.5, color:tok.dim, display:'flex', gap:16,
      }}>
        <span style={{color:tok.text}}>DAG[{tasks.length}]</span>
        <span>topological · top→down</span>
        <span><span style={{color:tok.green}}>●</span> ready · <span style={{color:tok.red}}>●</span> blocked · <span style={{color:tok.amber}}>●</span> in-flight</span>
      </div>
      <svg width={layout.width} height={layout.height} style={{display:'block'}}>
        <defs>
          <marker id="barrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0,0 L10,5 L0,10 z" fill={tok.line2}/>
          </marker>
          <marker id="barrowr" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0,0 L10,5 L0,10 z" fill={tok.red}/>
          </marker>
        </defs>
        {tasks.map(t => (t.blocked_by||[]).map(b => {
          const from = layout.pos[b], to = layout.pos[t.id];
          if (!from || !to) return null;
          const x1 = from.x+layout.W/2, y1=from.y+layout.H;
          const x2 = to.x+layout.W/2, y2=to.y;
          const mid = (y1+y2)/2;
          const blocked = tasksById[b]?.status !== 'done';
          return (
            <path key={t.id+'-'+b}
              d={`M ${x1} ${y1} L ${x1} ${mid} L ${x2} ${mid} L ${x2} ${y2}`}
              stroke={blocked?tok.red:tok.line2} strokeWidth={blocked?1.2:1}
              fill="none" markerEnd={`url(#${blocked?'barrowr':'barrow'})`}
              strokeDasharray={blocked?'0':'2 2'}/>
          );
        }))}
        {tasks.map(t => {
          const p = layout.pos[t.id]; if (!p) return null;
          const isReady = ready.has(t.id);
          const color = t.status==='in_progress'?tok.inprog:t.status==='done'?tok.done:t.status==='blocker'?tok.blocker:t.status==='closed'?tok.closed:tok.todo;
          const sel = selectedId===t.id;
          return (
            <g key={t.id} transform={`translate(${p.x} ${p.y})`} style={{cursor:'pointer'}}
               onClick={()=>setSelectedId(t.id)}>
              {isReady && (
                <rect x={-2} y={-2} width={layout.W+4} height={layout.H+4}
                  fill="none" stroke={tok.green} strokeDasharray="3 3" strokeWidth={1}/>
              )}
              <rect width={layout.W} height={layout.H}
                fill={sel ? tok.surface2 : tok.surface}
                stroke={sel ? tok.amber : tok.line2} strokeWidth={1}/>
              <text x={10} y={16} fontSize={9} fill={tok.dim} fontFamily='"JetBrains Mono", monospace'>
                {t.id} · P{t.priority} · {B_STATUS_META[t.status].mark}
              </text>
              <line x1={0} y1={24} x2={layout.W} y2={24} stroke={color} strokeWidth={1.5}/>
              <foreignObject x={10} y={28} width={layout.W-20} height={layout.H-32}>
                <div xmlns="http://www.w3.org/1999/xhtml" style={{
                  fontSize:11, color:tok.text, lineHeight:1.3,
                  fontFamily:'"IBM Plex Mono", "JetBrains Mono", monospace',
                  display:'-webkit-box', WebkitLineClamp:2, WebkitBoxOrient:'vertical', overflow:'hidden',
                }}>{t.title}</div>
              </foreignObject>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

function BDetail({ tok, task, tasksById, onClose, update }) {
  if (!task) return null;
  const deps = (task.blocked_by||[]).map(id=>tasksById[id]).filter(Boolean);
  const dependents = Object.values(tasksById).filter(t => (t.blocked_by||[]).includes(task.id));
  const proj = SEGMENTS_SEED.projects.find(p=>p.id===task.project);
  return (
    <aside style={{
      width:400, flexShrink:0, borderLeft:`1px solid ${tok.line}`, background:tok.bg,
      overflow:'auto', fontFamily:'"JetBrains Mono", monospace',
    }}>
      <div style={{
        padding:'8px 14px', borderBottom:`1px solid ${tok.line}`,
        display:'flex', justifyContent:'space-between', alignItems:'center',
        fontSize:10.5, color:tok.dim,
      }}>
        <span>$ task show {task.id}</span>
        <button onClick={onClose} style={{all:'unset', cursor:'pointer', color:tok.dim}}>[x]</button>
      </div>

      <div style={{ padding:'14px 14px 8px' }}>
        <div style={{ fontSize:10.5, color:tok.dim, marginBottom:6 }}>
          {proj?.name} / {task.id}
        </div>
        <h2 style={{
          margin:0, fontSize:15, fontWeight:500, color: tok.text, lineHeight:1.35,
          fontFamily:'"IBM Plex Mono", "JetBrains Mono", monospace',
        }}>{task.title}</h2>
      </div>

      <div style={{
        borderTop:`1px dashed ${tok.line}`, borderBottom:`1px dashed ${tok.line}`,
        padding:'10px 14px', display:'grid', gridTemplateColumns:'66px 1fr', rowGap:6, columnGap:8,
        fontSize:11, color:tok.dim,
      }}>
        <span>status</span>
        <span>
          <select value={task.status} onChange={e=>update(task.id,{status:e.target.value})}
            style={{ all:'unset', color:tok.text, cursor:'pointer', fontFamily:'inherit' }}>
            {Object.keys(B_STATUS_META).map(s => <option key={s} value={s} style={{background:tok.bg}}>{B_STATUS_META[s].mark} {B_STATUS_META[s].label}</option>)}
          </select>
        </span>
        <span>priority</span>
        <span style={{color:tok.text, display:'flex', gap:8, alignItems:'center'}}>
          <BPri p={task.priority} tok={tok}/> P{task.priority}
        </span>
        <span>updated</span>
        <span style={{color:tok.text}}>{SEGMENTS_UTIL.relTime(task.updated_at)}</span>
        <span>created</span>
        <span style={{color:tok.text}}>{SEGMENTS_UTIL.relTime(task.created_at)}</span>
        {task.closed_at && (<>
          <span>closed</span><span style={{color:tok.text}}>{SEGMENTS_UTIL.relTime(task.closed_at)}</span>
        </>)}
      </div>

      <div style={{ padding:'14px 14px', color:tok.text, fontSize:12, lineHeight:1.6, whiteSpace:'pre-wrap',
        fontFamily:'"IBM Plex Mono", "JetBrains Mono", monospace',
      }}>
        {task.body || <span style={{color:tok.faint}}>// no body</span>}
      </div>

      {(deps.length>0 || dependents.length>0) && (
        <div style={{padding:'10px 14px', borderTop:`1px dashed ${tok.line}`, fontSize:11}}>
          {deps.length>0 && <>
            <div style={{color:tok.dim, marginBottom:6}}>// blocked_by</div>
            {deps.map(d=>(
              <div key={d.id} style={{display:'flex', gap:8, padding:'3px 0', alignItems:'center', color:tok.text}}>
                <BPill status={d.status} tok={tok}/>
                <span style={{color:tok.dim, fontSize:10.5}}>{d.id}</span>
                <span style={{overflow:'hidden', whiteSpace:'nowrap', textOverflow:'ellipsis'}}>{d.title}</span>
              </div>
            ))}
          </>}
          {dependents.length>0 && <>
            <div style={{color:tok.dim, margin:'10px 0 6px'}}>// blocks</div>
            {dependents.map(d=>(
              <div key={d.id} style={{display:'flex', gap:8, padding:'3px 0', alignItems:'center', color:tok.text}}>
                <BPill status={d.status} tok={tok}/>
                <span style={{color:tok.dim, fontSize:10.5}}>{d.id}</span>
                <span style={{overflow:'hidden', whiteSpace:'nowrap', textOverflow:'ellipsis'}}>{d.title}</span>
              </div>
            ))}
          </>}
        </div>
      )}
    </aside>
  );
}

function BPalette({ tok, open, onClose, tasks, onJump, setView, theme, setTheme, update, selectedId }) {
  const [q, setQ] = useStateB('');
  const [idx, setIdx] = useStateB(0);
  const ref = useRefB(null);
  useEffectB(()=>{ if(open){setQ(''); setIdx(0); setTimeout(()=>ref.current?.focus(),10);} }, [open]);
  const items = useMemoB(() => {
    const base = [
      {type:'cmd', label:'view list', run:()=>setView('list')},
      {type:'cmd', label:'view kanban', run:()=>setView('kanban')},
      {type:'cmd', label:'view graph', run:()=>setView('graph')},
      {type:'cmd', label:`theme ${theme==='dark'?'light':'dark'}`, run:()=>setTheme(theme==='dark'?'light':'dark')},
      ...(selectedId ? [
        {type:'cmd', label:`mark ${selectedId} done`, run:()=>update(selectedId,{status:'done', closed_at:new Date().toISOString()})},
        {type:'cmd', label:`claim ${selectedId}`, run:()=>update(selectedId,{status:'in_progress'})},
      ]:[]),
      ...tasks.slice(0,40).map(t=>({type:'jump', label: t.title, sub: t.id, run:()=>onJump(t.id)})),
    ];
    if (!q) return base;
    const nq = q.toLowerCase();
    return base.filter(i => i.label.toLowerCase().includes(nq) || (i.sub||'').toLowerCase().includes(nq));
  }, [q, tasks, theme, selectedId]);
  if (!open) return null;
  return (
    <div onClick={onClose} style={{
      position:'absolute', inset:0, background:'rgba(0,0,0,0.55)', zIndex:100,
      display:'flex', justifyContent:'center', alignItems:'flex-start', paddingTop:80,
    }}>
      <div onClick={e=>e.stopPropagation()} style={{
        width:560, background:tok.bg, border:`1px solid ${tok.line2}`, fontFamily:'"JetBrains Mono", monospace',
      }}>
        <div style={{display:'flex', alignItems:'center', padding:'10px 14px', borderBottom:`1px solid ${tok.line}`, gap:6}}>
          <span style={{color:tok.amber, fontSize:12}}>$</span>
          <input ref={ref} value={q} onChange={e=>{setQ(e.target.value); setIdx(0);}}
            onKeyDown={e=>{
              if(e.key==='Escape') onClose();
              else if(e.key==='ArrowDown'){setIdx(i=>Math.min(items.length-1,i+1)); e.preventDefault();}
              else if(e.key==='ArrowUp'){setIdx(i=>Math.max(0,i-1)); e.preventDefault();}
              else if(e.key==='Enter'){ items[idx]?.run(); onClose(); e.preventDefault();}
            }}
            placeholder="cmd or task…"
            style={{all:'unset', flex:1, color:tok.text, fontSize:13, fontFamily:'inherit'}}/>
        </div>
        <div style={{maxHeight:360, overflow:'auto'}}>
          {items.slice(0,30).map((it,i) => (
            <div key={i} onMouseEnter={()=>setIdx(i)} onClick={()=>{it.run(); onClose();}}
              style={{
                padding:'6px 14px', fontSize:11.5,
                color: tok.text, background: idx===i ? tok.surface2 : 'transparent',
                borderLeft:`2px solid ${idx===i ? tok.amber : 'transparent'}`,
                cursor:'pointer', display:'flex', justifyContent:'space-between',
              }}>
              <span><span style={{color:tok.dim, marginRight:8}}>{it.type}</span>{it.label}</span>
              {it.sub && <span style={{color:tok.dim}}>{it.sub}</span>}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function BComposer({ tok, open, onClose, onCreate }) {
  const [title, setTitle] = useStateB('');
  const [pri, setPri] = useStateB(1);
  const ref = useRefB(null);
  useEffectB(()=>{ if(open){setTitle(''); setPri(1); setTimeout(()=>ref.current?.focus(),10);}}, [open]);
  if (!open) return null;
  const submit = () => {
    if (!title.trim()) return;
    const id = 'SEG-'+(500+Math.floor(Math.random()*500));
    onCreate({
      id, project:'proj_segments', title:title.trim(), body:'',
      priority:pri, status:'todo', blocked_by:[],
      created_at:new Date().toISOString(), updated_at:new Date().toISOString(),
      closed_at:null, sort_order:0,
    });
    onClose();
  };
  return (
    <div style={{
      borderBottom:`1px solid ${tok.line}`, background:tok.surface, padding:'8px 14px',
      fontFamily:'"JetBrains Mono", monospace', display:'flex', gap:10, alignItems:'center', fontSize:12,
    }}>
      <span style={{color:tok.amber}}>$ create</span>
      <input ref={ref} value={title} onChange={e=>setTitle(e.target.value)}
        onKeyDown={e=>{ if(e.key==='Enter') submit(); if(e.key==='Escape') onClose(); }}
        placeholder="task title…"
        style={{all:'unset', flex:1, color:tok.text, fontFamily:'inherit'}}/>
      {[0,1,2,3].map(p => (
        <button key={p} onClick={()=>setPri(p)} style={{
          all:'unset', cursor:'pointer', padding:'1px 6px', fontSize:10.5,
          border:`1px solid ${pri===p ? tok.amber : tok.line}`,
          color: pri===p ? tok.amber : tok.dim,
        }}>P{p}</button>
      ))}
      <span style={{color:tok.faint, fontSize:10}}>↵ ok · esc quit</span>
    </div>
  );
}

function SegmentsB() {
  const [theme, setTheme] = useStateB('dark');
  const tok = B_TOK[theme];
  const [view, setView] = useStateB('list');
  const [search, setSearch] = useStateB('');
  const [selectedId, setSelectedId] = useStateB('SEG-105');
  const [activeProject, setActiveProject] = useStateB(null);
  const [paletteOpen, setPaletteOpen] = useStateB(false);
  const [composerOpen, setComposerOpen] = useStateB(false);
  const [agentOn, setAgentOn] = useStateB(true);

  // Reuse useTasks hook from direction A app
  const { tasks, update, add, pulse, entering } = useTasks(SEGMENTS_SEED);
  const agentLog = useAgentSim({ tasks, update, add, enabled: agentOn });

  const tasksById = useMemoB(()=>Object.fromEntries(tasks.map(t=>[t.id,t])), [tasks]);
  const tasksByProject = useMemoB(()=>{
    const m={}; SEGMENTS_SEED.projects.forEach(p=>m[p.id]=[]);
    tasks.forEach(t=>(m[t.project]=m[t.project]||[]).push(t));
    return m;
  }, [tasks]);

  const filtered = useMemoB(() => {
    let o = tasks;
    if (activeProject) o = o.filter(t=>t.project===activeProject);
    if (search) o = SEGMENTS_UTIL.fuzzy(search, o);
    return o;
  }, [tasks, activeProject, search]);

  const counts = useMemoB(() => {
    const c = SEGMENTS_UTIL.counts(tasks);
    return {
      ...c, total: tasks.length,
      ready: SEGMENTS_UTIL.ready(tasks).length,
      lastWrite: tasks.length ? SEGMENTS_UTIL.relTime(tasks.map(t=>t.updated_at).sort().pop()) : '-',
    };
  }, [tasks]);

  useEffectB(() => {
    let gPending = false, gT;
    const handler = (e) => {
      const target = e.target;
      const isInput = target && (target.tagName==='INPUT' || target.tagName==='TEXTAREA');
      if (e.key==='Escape') {
        if (paletteOpen) setPaletteOpen(false);
        else if (composerOpen) setComposerOpen(false);
        else if (selectedId) setSelectedId(null);
        return;
      }
      if ((e.metaKey||e.ctrlKey) && e.key.toLowerCase()==='k') { e.preventDefault(); setPaletteOpen(p=>!p); return; }
      if (isInput) return;
      if (e.key==='/') { e.preventDefault(); document.querySelector('[data-search-input]')?.focus(); return; }
      if (e.key==='c') { e.preventDefault(); setComposerOpen(true); return; }
      if (gPending) {
        clearTimeout(gT); gPending = false;
        if (e.key==='k') setView('kanban');
        else if (e.key==='l') setView('list');
        else if (e.key==='g') setView('graph');
        return;
      }
      if (e.key==='g') { gPending = true; gT = setTimeout(()=>gPending=false, 800); return; }
      if (e.key==='j' || e.key==='k') {
        const idx = filtered.findIndex(t=>t.id===selectedId);
        const next = e.key==='j' ? Math.min(filtered.length-1, idx+1) : Math.max(0, idx-1);
        if (filtered[next]) setSelectedId(filtered[next].id);
        e.preventDefault(); return;
      }
      if (['1','2','3','4'].includes(e.key) && selectedId) update(selectedId, {priority:parseInt(e.key)-1});
      if (e.key==='t' && selectedId) {
        const order = ['todo','in_progress','blocker','done','closed'];
        const cur = tasksById[selectedId]?.status;
        update(selectedId, {status: order[(order.indexOf(cur)+1)%order.length]});
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [paletteOpen, composerOpen, selectedId, filtered, tasksById, update]);

  return (
    <div style={{
      height:'100vh', width:'100vw', background:tok.bg, color:tok.text,
      fontFamily:'"JetBrains Mono", "IBM Plex Mono", ui-monospace, monospace',
      display:'flex', flexDirection:'column', overflow:'hidden',
    }}>
      <BTop tok={tok} theme={theme} setTheme={setTheme} view={view} setView={setView}
        search={search} setSearch={setSearch}
        onPalette={()=>setPaletteOpen(true)} agentOn={agentOn} setAgentOn={setAgentOn}
        counts={counts}/>
      <div style={{ display:'flex', flex:1, minHeight:0 }}>
        <BSide tok={tok} projects={SEGMENTS_SEED.projects} tasksByProject={tasksByProject}
          activeProject={activeProject} setActiveProject={setActiveProject} agentLog={agentLog}/>
        <div style={{ flex:1, display:'flex', flexDirection:'column', minWidth:0 }}>
          <BComposer tok={tok} open={composerOpen} onClose={()=>setComposerOpen(false)} onCreate={add}/>
          {view==='list'   && <BListView tok={tok} tasks={filtered} tasksById={tasksById} selectedId={selectedId} setSelectedId={setSelectedId} pulse={pulse} entering={entering}/>}
          {view==='kanban' && <BKanban tok={tok} tasks={filtered} tasksById={tasksById} selectedId={selectedId} setSelectedId={setSelectedId} update={update} pulse={pulse}/>}
          {view==='graph'  && <BGraph  tok={tok} tasks={filtered} tasksById={tasksById} selectedId={selectedId} setSelectedId={setSelectedId}/>}
        </div>
        {selectedId && tasksById[selectedId] && (
          <BDetail tok={tok} task={tasksById[selectedId]} tasksById={tasksById} onClose={()=>setSelectedId(null)} update={update}/>
        )}
      </div>
      <BPalette tok={tok} open={paletteOpen} onClose={()=>setPaletteOpen(false)}
        tasks={tasks} onJump={setSelectedId} setView={setView}
        theme={theme} setTheme={setTheme} update={update} selectedId={selectedId}/>
    </div>
  );
}

window.SegmentsB = SegmentsB;
