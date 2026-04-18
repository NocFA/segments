/* Direction C — "Dossier"
   Editorial / brutalist. Serif display, grotesk body.
   Paper & ink. Oversized numerals. Rubber-stamp accents.
*/
const { useState: useStateC, useEffect: useEffectC, useMemo: useMemoC, useRef: useRefC } = React;

const C_TOK = {
  light: {
    bg:       '#efece4',
    paper:    '#f8f5ec',
    ink:      '#161312',
    ink2:     '#2e2a27',
    mute:     '#6d665c',
    faint:    '#a49d8f',
    rule:     '#161312',
    rule2:    '#b3ac9e',
    red:      '#a8291f',
    ochre:    '#8a6a1a',
    moss:     '#3a5a2e',
    blue:     '#1f3d6a',
    todo:     '#6d665c',
    inprog:   '#8a6a1a',
    blocker:  '#a8291f',
    done:     '#3a5a2e',
    closed:   '#a49d8f',
    stampRed: '#a8291f',
  },
  dark: {
    bg:       '#14110e',
    paper:    '#1a1713',
    ink:      '#eae2d0',
    ink2:     '#c8bfa9',
    mute:     '#8f8775',
    faint:    '#5a5447',
    rule:     '#eae2d0',
    rule2:    '#4a4338',
    red:      '#d9604f',
    ochre:    '#d9a650',
    moss:     '#8cb56f',
    blue:     '#7aa7d9',
    todo:     '#8f8775',
    inprog:   '#d9a650',
    blocker:  '#d9604f',
    done:     '#8cb56f',
    closed:   '#5a5447',
    stampRed: '#d9604f',
  }
};

const C_STATUS_META = {
  todo:        { label:'TO DO',        ornament:'◇' },
  in_progress: { label:'IN COURSE',    ornament:'◈' },
  blocker:     { label:'ON HOLD',      ornament:'◆' },
  done:        { label:'SETTLED',      ornament:'❖' },
  closed:      { label:'WITHDRAWN',    ornament:'▫' },
};

function CPriNum({ p, tok }) {
  const roman = ['I','II','III','IV'];
  const colors = [tok.red, tok.ochre, tok.blue, tok.faint];
  return (
    <span style={{
      fontFamily:'"Instrument Serif", "Times New Roman", serif',
      fontStyle:'italic', fontWeight:400,
      fontSize:22, color: colors[p], letterSpacing:0.5,
      lineHeight:1, minWidth:28, display:'inline-block',
    }}>{roman[p]}</span>
  );
}

function CStatusTag({ status, tok, pulsing }) {
  const meta = C_STATUS_META[status];
  const color = tok[status==='in_progress'?'inprog':status];
  return (
    <span className={pulsing?'seg-pulse-c':''} style={{
      fontFamily:'"Space Grotesk", system-ui, sans-serif',
      fontSize:10, letterSpacing:2, fontWeight:500,
      color, display:'inline-flex', alignItems:'center', gap:5,
      textTransform:'uppercase',
    }}>
      <span style={{ fontSize:12, color }}>{meta.ornament}</span>
      {meta.label}
    </span>
  );
}

function CBlockerChip({ ids, tasksById, tok }) {
  if (!ids || !ids.length) return null;
  const unresolved = ids.some(id => tasksById[id] && tasksById[id].status !== 'done');
  if (unresolved) {
    return (
      <span style={{
        fontFamily:'"Space Grotesk", sans-serif', fontSize:9.5, fontWeight:600,
        color: tok.stampRed, letterSpacing:1.5,
        border:`1.5px solid ${tok.stampRed}`, padding:'1px 5px', borderRadius:2,
        transform:'rotate(-2deg)', display:'inline-block',
      }}>HELD BY {ids.join('·')}</span>
    );
  }
  return <span style={{ color:tok.faint, fontFamily:'"Space Grotesk", sans-serif', fontSize:10 }}>
    ✓ {ids.join('·')}
  </span>;
}

function CRow({ task, tasksById, tok, selected, onClick, pulsing, entering, idx }) {
  const body1 = SEGMENTS_UTIL.firstLine(task.body);
  return (
    <article onClick={onClick}
      className={entering ? 'seg-enter-c' : ''}
      style={{
        display:'grid',
        gridTemplateColumns:'52px 38px 112px 1fr auto 82px',
        gap:16, alignItems:'center',
        padding:'14px 24px',
        borderBottom:`0.5px solid ${tok.rule2}`,
        background: selected ? tok.paper : 'transparent',
        cursor:'pointer', position:'relative',
      }}
    >
      <span style={{
        fontFamily:'"Instrument Serif", serif',
        fontSize:13, color: tok.faint, fontVariantNumeric:'tabular-nums',
        fontStyle:'italic',
      }}>№ {String(idx+1).padStart(2,'0')}</span>
      <CPriNum p={task.priority} tok={tok}/>
      <CStatusTag status={task.status} tok={tok} pulsing={pulsing}/>
      <div style={{ minWidth:0 }}>
        <div style={{
          fontFamily:'"Instrument Serif", "Times New Roman", serif',
          fontSize:17, lineHeight:1.25, color: task.status==='closed'?tok.faint:tok.ink,
          textDecoration: task.status==='closed' ? 'line-through' : 'none',
          fontWeight:400,
          whiteSpace:'nowrap', overflow:'hidden', textOverflow:'ellipsis',
          letterSpacing:-0.2,
        }}>{task.title}</div>
        {body1 && (
          <div style={{
            fontFamily:'"Space Grotesk", system-ui, sans-serif',
            fontSize:11.5, color: tok.mute, marginTop:2,
            whiteSpace:'nowrap', overflow:'hidden', textOverflow:'ellipsis',
          }}>{body1}</div>
        )}
      </div>
      <CBlockerChip ids={task.blocked_by} tasksById={tasksById} tok={tok}/>
      <span style={{
        fontFamily:'"Space Grotesk", sans-serif', fontSize:10.5,
        color: tok.mute, textAlign:'right', letterSpacing:0.5, textTransform:'uppercase',
      }}>
        <div style={{color:tok.faint, fontSize:9}}>{task.id}</div>
        {SEGMENTS_UTIL.relTime(task.updated_at)}
      </span>
    </article>
  );
}

function CTop({ tok, theme, setTheme, view, setView, search, setSearch, onPalette, agentOn, setAgentOn, counts }) {
  return (
    <header style={{ borderBottom:`2px solid ${tok.rule}`, background: tok.bg }}>
      {/* Masthead */}
      <div style={{
        padding:'18px 24px 12px', display:'grid',
        gridTemplateColumns:'1fr auto 1fr', alignItems:'end',
      }}>
        <div style={{
          fontFamily:'"Space Grotesk", sans-serif', fontSize:10, letterSpacing:3,
          textTransform:'uppercase', color: tok.mute,
        }}>
          VOL. I · No. {counts.total} · {new Date().toISOString().slice(0,10)}
        </div>
        <div style={{
          fontFamily:'"Instrument Serif", serif',
          fontSize:44, lineHeight:1, color: tok.ink, letterSpacing:-1,
          textAlign:'center',
        }}>
          Segments <span style={{fontStyle:'italic', fontSize:24, color:tok.mute}}>— a register of work</span>
        </div>
        <div style={{
          fontFamily:'"Space Grotesk", sans-serif', fontSize:10, letterSpacing:2,
          textTransform:'uppercase', color: tok.mute, textAlign:'right',
          display:'flex', justifyContent:'flex-end', gap:14, alignItems:'center',
        }}>
          <label style={{ display:'flex', gap:6, alignItems:'center', cursor:'pointer' }}>
            <span style={{
              width:6, height:6, borderRadius:3, background: agentOn ? tok.stampRed : tok.faint,
            }} className={agentOn?'seg-blink-c':''}/>
            <span>Agent {agentOn?'Live':'Paused'}</span>
            <input type="checkbox" checked={agentOn} onChange={e=>setAgentOn(e.target.checked)} style={{display:'none'}}/>
          </label>
          <button onClick={()=>setTheme(theme==='dark'?'light':'dark')} style={{
            all:'unset', cursor:'pointer', fontFamily:'inherit', fontSize:10, letterSpacing:2, color:tok.mute,
            border:`1px solid ${tok.rule2}`, padding:'2px 6px',
          }}>{theme==='dark'?'LIGHT':'DARK'}</button>
        </div>
      </div>
      {/* Heavy rule + stats */}
      <div style={{ borderTop:`1px solid ${tok.rule}`, padding:'6px 24px',
        display:'flex', gap:20, fontFamily:'"Space Grotesk", sans-serif', fontSize:10, letterSpacing:1,
        color: tok.mute, textTransform:'uppercase',
      }}>
        <span><strong style={{color:tok.ink}}>{counts.in_progress}</strong> in course</span>
        <span>·</span>
        <span><strong style={{color:tok.stampRed}}>{counts.blocker}</strong> on hold</span>
        <span>·</span>
        <span><strong style={{color:tok.ink}}>{counts.todo}</strong> outstanding</span>
        <span>·</span>
        <span><strong style={{color:tok.moss}}>{counts.done}</strong> settled</span>
        <span style={{flex:1}}/>
        <span>ready · <strong style={{color:tok.ink}}>{counts.ready}</strong></span>
      </div>
      {/* Controls row */}
      <div style={{
        borderTop:`0.5px solid ${tok.rule2}`, padding:'10px 24px', display:'flex', gap:14, alignItems:'center',
      }}>
        <nav style={{ display:'flex', gap:2 }}>
          {[['list','List', 'gl'],['kanban','Board','gk'],['graph','Lineage','gg']].map(([v,l,k]) => (
            <button key={v} onClick={()=>setView(v)} style={{
              all:'unset', cursor:'pointer',
              padding:'6px 14px',
              fontFamily:'"Instrument Serif", serif',
              fontSize:16, fontStyle: view===v?'italic':'normal',
              color: view===v ? tok.ink : tok.mute,
              borderBottom: `2px solid ${view===v ? tok.ink : 'transparent'}`,
            }}>
              {l} <span style={{fontFamily:'"Space Grotesk", sans-serif', fontSize:9, letterSpacing:1, color:tok.faint}}>{k}</span>
            </button>
          ))}
        </nav>
        <div style={{ flex:1 }}/>
        <div style={{
          display:'flex', alignItems:'center', gap:8,
          borderBottom:`1px solid ${tok.rule}`, padding:'3px 0',
          minWidth:300, fontFamily:'"Space Grotesk", sans-serif',
        }}>
          <span style={{ fontSize:10, letterSpacing:2, color:tok.mute }}>SEARCH —</span>
          <input data-search-input value={search} onChange={e=>setSearch(e.target.value)}
            placeholder="title, description, or id" style={{
            all:'unset', flex:1, fontSize:13, color:tok.ink,
          }}/>
        </div>
        <button onClick={onPalette} style={{
          all:'unset', cursor:'pointer',
          fontFamily:'"Space Grotesk", sans-serif', fontSize:10, letterSpacing:2,
          border:`1px solid ${tok.rule}`, padding:'4px 8px', color:tok.ink,
        }}>⌘K</button>
      </div>
    </header>
  );
}

function CSide({ tok, projects, tasksByProject, activeProject, setActiveProject, readyCount }) {
  return (
    <aside style={{
      width:240, borderRight:`2px solid ${tok.rule}`, padding:'20px 20px', flexShrink:0,
    }}>
      <div style={{
        fontFamily:'"Instrument Serif", serif', fontSize:13, fontStyle:'italic',
        color: tok.mute, marginBottom:10, letterSpacing:0.2,
      }}>— Projects —</div>
      <div style={{ display:'flex', flexDirection:'column', gap:2 }}>
        <button onClick={()=>setActiveProject(null)} style={{
          all:'unset', cursor:'pointer', padding:'6px 0',
          fontFamily:'"Instrument Serif", serif',
          fontSize:17, color: !activeProject ? tok.ink : tok.mute,
          fontStyle: !activeProject ? 'italic' : 'normal',
          borderBottom:`0.5px dashed ${tok.rule2}`,
          display:'flex', justifyContent:'space-between', alignItems:'baseline',
        }}>
          <span>All entries</span>
          <span style={{fontFamily:'"Space Grotesk", sans-serif', fontSize:10, color:tok.mute}}>
            {Object.values(tasksByProject).reduce((a,b)=>a+b.length,0)}
          </span>
        </button>
        {projects.map(p => {
          const ts = tasksByProject[p.id] || [];
          const c = SEGMENTS_UTIL.counts(ts);
          const active = activeProject === p.id;
          return (
            <button key={p.id} onClick={()=>setActiveProject(active?null:p.id)} style={{
              all:'unset', cursor:'pointer', padding:'6px 0',
              borderBottom:`0.5px dashed ${tok.rule2}`,
            }}>
              <div style={{
                display:'flex', justifyContent:'space-between', alignItems:'baseline',
              }}>
                <span style={{
                  fontFamily:'"Instrument Serif", serif', fontSize:17,
                  color: active ? tok.ink : tok.ink2,
                  fontStyle: active ? 'italic' : 'normal',
                }}>
                  {active && <span style={{marginRight:6}}>›</span>}{p.name}
                </span>
                <span style={{fontFamily:'"Space Grotesk", sans-serif', fontSize:10, color:tok.mute}}>
                  {ts.length}
                </span>
              </div>
              <div style={{
                display:'flex', gap:6, marginTop:3, fontFamily:'"Space Grotesk", sans-serif',
                fontSize:9, letterSpacing:1, color:tok.mute, textTransform:'uppercase',
              }}>
                {c.in_progress>0 && <span>◈ {c.in_progress}</span>}
                {c.blocker>0 && <span style={{color:tok.stampRed}}>◆ {c.blocker}</span>}
                {c.todo>0 && <span>◇ {c.todo}</span>}
                {c.done>0 && <span style={{color:tok.moss}}>❖ {c.done}</span>}
              </div>
            </button>
          );
        })}
      </div>

      <div style={{
        marginTop:20, padding:'12px 14px', border:`1.5px solid ${tok.stampRed}`,
        transform:'rotate(-1deg)',
      }}>
        <div style={{
          fontFamily:'"Space Grotesk", sans-serif', fontSize:9, letterSpacing:2,
          color: tok.stampRed, fontWeight:600,
        }}>READY QUEUE</div>
        <div style={{
          fontFamily:'"Instrument Serif", serif',
          fontSize:36, color: tok.stampRed, lineHeight:1, marginTop:4, fontStyle:'italic',
        }}>{readyCount}</div>
        <div style={{
          fontFamily:'"Space Grotesk", sans-serif', fontSize:10, color:tok.mute, marginTop:4,
        }}>entries with no outstanding dependencies</div>
      </div>

      <div style={{
        marginTop:20, fontFamily:'"Space Grotesk", sans-serif', fontSize:9.5, color:tok.mute,
        lineHeight:1.8, letterSpacing:0.5,
      }}>
        <div style={{color:tok.ink, marginBottom:4, letterSpacing:1, textTransform:'uppercase'}}>Keys</div>
        j · k &nbsp; navigate<br/>
        c &nbsp;&nbsp; compose<br/>
        e &nbsp;&nbsp; edit<br/>
        / &nbsp;&nbsp; search<br/>
        t &nbsp;&nbsp; cycle status<br/>
        1–4 priority<br/>
        g then k / l / g<br/>
        ⌘K palette
      </div>
    </aside>
  );
}

function CListView({ tok, tasks, tasksById, selectedId, setSelectedId, pulse, entering, groupBy='status' }) {
  const groups = useMemoC(() => {
    if (groupBy==='none') return [{key:'all', items:tasks}];
    const order = ['in_progress','blocker','todo','done','closed'];
    const buckets={}; order.forEach(s=>buckets[s]=[]);
    tasks.forEach(t=>buckets[t.status].push(t));
    return order.filter(s=>buckets[s].length).map(s=>({key:s, items:buckets[s]}));
  }, [tasks, groupBy]);
  let runningIdx = 0;
  return (
    <div style={{ flex:1, overflow:'auto' }}>
      {groups.map(g => (
        <section key={g.key}>
          <div style={{
            padding:'22px 24px 8px',
            fontFamily:'"Instrument Serif", serif',
            fontSize:11, letterSpacing:3, textTransform:'uppercase',
            color: tok[g.key==='in_progress'?'inprog':g.key] || tok.mute,
            borderBottom:`0.5px solid ${tok.rule}`,
            display:'flex', alignItems:'baseline', justifyContent:'space-between',
            background: tok.bg, position:'sticky', top:0, zIndex:1,
          }}>
            <span>
              <span style={{ fontSize:14, marginRight:8 }}>{C_STATUS_META[g.key]?.ornament}</span>
              {C_STATUS_META[g.key]?.label}
            </span>
            <span style={{ fontFamily:'"Space Grotesk", sans-serif', fontSize:10, color:tok.mute }}>
              {g.items.length} {g.items.length===1?'entry':'entries'}
            </span>
          </div>
          {g.items.map(t => {
            const idx = runningIdx++;
            return (
              <CRow key={t.id} task={t} tasksById={tasksById} tok={tok}
                selected={selectedId===t.id}
                pulsing={pulse[t.id] && Date.now()-pulse[t.id]<2000}
                entering={!!entering[t.id]}
                onClick={()=>setSelectedId(t.id)} idx={idx}/>
            );
          })}
        </section>
      ))}
    </div>
  );
}

function CKanban({ tok, tasks, tasksById, selectedId, setSelectedId, update, pulse }) {
  const cols = ['todo','in_progress','blocker','done'];
  const [dragging, setDragging] = useStateC(null);
  const [overCol, setOverCol] = useStateC(null);
  return (
    <div style={{ flex:1, display:'grid', gridTemplateColumns:`repeat(${cols.length}, 1fr)`, overflow:'auto' }}>
      {cols.map(c => {
        const items = tasks.filter(t=>t.status===c);
        const color = tok[c==='in_progress'?'inprog':c];
        return (
          <div key={c}
            onDragOver={e=>{e.preventDefault(); setOverCol(c);}}
            onDragLeave={()=>setOverCol(null)}
            onDrop={e=>{
              e.preventDefault();
              const id = e.dataTransfer.getData('text/plain');
              if (id) update(id,{status:c});
              setDragging(null); setOverCol(null);
            }}
            style={{
              borderRight:`1px solid ${tok.rule2}`,
              background: overCol===c ? tok.paper : 'transparent',
              display:'flex', flexDirection:'column',
            }}>
            <div style={{
              padding:'18px 18px 10px',
              borderBottom:`1px solid ${tok.rule}`,
              display:'flex', flexDirection:'column', gap:4,
              position:'sticky', top:0, background:tok.bg, zIndex:1,
            }}>
              <div style={{
                fontFamily:'"Space Grotesk", sans-serif', fontSize:9, letterSpacing:3,
                color: tok.mute, textTransform:'uppercase',
              }}>Column {'ABCD'[cols.indexOf(c)]}</div>
              <div style={{
                fontFamily:'"Instrument Serif", serif',
                fontSize:26, color: color, lineHeight:1, letterSpacing:-0.5, fontStyle:'italic',
                display:'flex', justifyContent:'space-between', alignItems:'baseline',
              }}>
                <span>{C_STATUS_META[c].label.toLowerCase()}</span>
                <span style={{fontSize:14, color:tok.mute}}>{items.length}</span>
              </div>
            </div>
            <div style={{padding:12, display:'flex', flexDirection:'column', gap:10}}>
              {items.map(t => (
                <article key={t.id} draggable
                  onDragStart={e=>{setDragging(t.id); e.dataTransfer.setData('text/plain', t.id);}}
                  onClick={()=>setSelectedId(t.id)}
                  className={pulse[t.id] && Date.now()-pulse[t.id]<2000 ? 'seg-pulse-c':''}
                  style={{
                    padding:'12px 14px', background:tok.paper,
                    border: selectedId===t.id ? `1.5px solid ${tok.ink}` : `0.5px solid ${tok.rule2}`,
                    cursor:'grab', opacity: dragging===t.id ? 0.4 : 1,
                    position:'relative',
                  }}>
                  <div style={{ display:'flex', justifyContent:'space-between', alignItems:'flex-start', marginBottom:8 }}>
                    <CPriNum p={t.priority} tok={tok}/>
                    <span style={{
                      fontFamily:'"Space Grotesk", sans-serif', fontSize:9,
                      color:tok.faint, letterSpacing:1,
                    }}>{t.id}</span>
                  </div>
                  <h3 style={{
                    margin:0, fontFamily:'"Instrument Serif", serif',
                    fontSize:15, lineHeight:1.25, fontWeight:400, letterSpacing:-0.1,
                    color: tok.ink, marginBottom:10,
                  }}>{t.title}</h3>
                  <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center' }}>
                    <CBlockerChip ids={t.blocked_by} tasksById={tasksById} tok={tok}/>
                    <span style={{ fontFamily:'"Space Grotesk", sans-serif', fontSize:9.5, letterSpacing:1, color: tok.mute, textTransform:'uppercase' }}>
                      {SEGMENTS_UTIL.relTime(t.updated_at)}
                    </span>
                  </div>
                </article>
              ))}
              {items.length===0 && (
                <div style={{
                  fontFamily:'"Instrument Serif", serif', fontStyle:'italic',
                  fontSize:14, color: tok.faint, padding:24, textAlign:'center',
                }}>— vacat —</div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function CGraph({ tok, tasks, tasksById, selectedId, setSelectedId }) {
  const layout = useMemoC(() => {
    const rank = {};
    const visit = (id, seen=new Set()) => {
      if (rank[id] !== undefined) return rank[id];
      if (seen.has(id)) return 0;
      seen.add(id);
      const t = tasksById[id];
      if (!t || !t.blocked_by || !t.blocked_by.length) { rank[id]=0; return 0; }
      const r = 1 + Math.max(0, ...t.blocked_by.filter(b=>tasksById[b]).map(b=>visit(b,seen)));
      rank[id]=r; return r;
    };
    tasks.forEach(t=>visit(t.id));
    const rows={};
    tasks.forEach(t=>{ const r=rank[t.id]||0; (rows[r]=rows[r]||[]).push(t); });
    const keys=Object.keys(rows).map(Number).sort((a,b)=>a-b);
    const W=210, H=78, gapX=40, gapY=56;
    const pos={};
    keys.forEach(r=>{
      rows[r].sort((a,b)=>a.priority-b.priority);
      rows[r].forEach((t,i)=>{ pos[t.id]={x:50+i*(W+gapX), y:50+r*(H+gapY)}; });
    });
    const width=Math.max(...Object.values(pos).map(p=>p.x+W))+50;
    const height=50+keys.length*(H+gapY);
    return {pos,W,H,width,height};
  }, [tasks, tasksById]);
  const ready = useMemoC(()=>new Set(SEGMENTS_UTIL.ready(tasks).map(t=>t.id)), [tasks]);
  return (
    <div style={{ flex:1, overflow:'auto', background: tok.bg }}>
      <div style={{
        padding:'18px 24px 10px',
        position:'sticky', top:0, background:tok.bg, zIndex:2,
        borderBottom:`1px solid ${tok.rule}`,
      }}>
        <div style={{
          fontFamily:'"Instrument Serif", serif', fontSize:24, color: tok.ink,
          fontStyle:'italic', letterSpacing:-0.3,
        }}>Lineage</div>
        <div style={{
          fontFamily:'"Space Grotesk", sans-serif', fontSize:10, color:tok.mute,
          letterSpacing:1.2, textTransform:'uppercase', marginTop:4,
        }}>
          dependency graph · {ready.size} of {tasks.length} ready
        </div>
      </div>
      <svg width={layout.width} height={layout.height} style={{display:'block'}}>
        <defs>
          <marker id="carrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto">
            <path d="M0,0 L10,5 L0,10 z" fill={tok.rule}/>
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
              d={`M ${x1} ${y1} C ${x1} ${mid} ${x2} ${mid} ${x2} ${y2}`}
              stroke={blocked?tok.stampRed:tok.rule} strokeWidth={blocked?1.5:0.75} fill="none"
              strokeDasharray={blocked?'0':'3 4'}
              markerEnd="url(#carrow)"/>
          );
        }))}
        {tasks.map(t => {
          const p = layout.pos[t.id]; if (!p) return null;
          const isReady = ready.has(t.id);
          const color = t.status==='in_progress'?tok.inprog:t.status==='done'?tok.moss:t.status==='blocker'?tok.stampRed:t.status==='closed'?tok.closed:tok.ink;
          const sel = selectedId===t.id;
          return (
            <g key={t.id} transform={`translate(${p.x} ${p.y})`} style={{cursor:'pointer'}}
               onClick={()=>setSelectedId(t.id)}>
              {isReady && (
                <rect x={-4} y={-4} width={layout.W+8} height={layout.H+8}
                  fill="none" stroke={tok.stampRed} strokeWidth={0.75} strokeDasharray="2 3"/>
              )}
              <rect width={layout.W} height={layout.H}
                fill={sel ? tok.paper : tok.bg}
                stroke={sel ? tok.ink : tok.rule} strokeWidth={sel?1.5:0.5}/>
              <line x1={12} y1={17} x2={layout.W-12} y2={17} stroke={color} strokeWidth={1}/>
              <text x={12} y={14} fontSize={8.5} fill={tok.mute}
                fontFamily='"Space Grotesk", sans-serif' letterSpacing="1">
                {t.id} · P{t.priority} · {C_STATUS_META[t.status].label}
              </text>
              <foreignObject x={12} y={22} width={layout.W-24} height={layout.H-28}>
                <div xmlns="http://www.w3.org/1999/xhtml" style={{
                  fontFamily:'"Instrument Serif", serif',
                  fontSize:13, color:tok.ink, lineHeight:1.25, fontWeight:400,
                  display:'-webkit-box', WebkitLineClamp:3, WebkitBoxOrient:'vertical', overflow:'hidden',
                }}>{t.title}</div>
              </foreignObject>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

function CDetail({ tok, task, tasksById, onClose, update }) {
  if (!task) return null;
  const deps = (task.blocked_by||[]).map(id=>tasksById[id]).filter(Boolean);
  const dependents = Object.values(tasksById).filter(t => (t.blocked_by||[]).includes(task.id));
  const proj = SEGMENTS_SEED.projects.find(p=>p.id===task.project);
  return (
    <aside style={{
      width:440, flexShrink:0, borderLeft:`2px solid ${tok.rule}`, background:tok.bg,
      overflow:'auto',
    }}>
      <div style={{
        padding:'14px 24px', borderBottom:`0.5px solid ${tok.rule2}`,
        display:'flex', justifyContent:'space-between', alignItems:'center',
        fontFamily:'"Space Grotesk", sans-serif', fontSize:10, letterSpacing:2,
        color: tok.mute, textTransform:'uppercase',
      }}>
        <span>Entry / {proj?.name} / {task.id}</span>
        <button onClick={onClose} style={{all:'unset', cursor:'pointer', color: tok.ink, fontSize:16}}>×</button>
      </div>

      <div style={{ padding:'20px 24px 8px', position:'relative' }}>
        <div style={{ display:'flex', alignItems:'center', gap:12, marginBottom:10 }}>
          <CPriNum p={task.priority} tok={tok}/>
          <CStatusTag status={task.status} tok={tok}/>
        </div>
        <h1 style={{
          margin:0, fontFamily:'"Instrument Serif", "Times New Roman", serif',
          fontSize:26, fontWeight:400, color: tok.ink, lineHeight:1.15, letterSpacing:-0.5,
        }}>{task.title}</h1>

        {task.status==='blocker' && (
          <div style={{
            position:'absolute', top:20, right:24,
            border:`2px solid ${tok.stampRed}`, padding:'4px 10px',
            transform:'rotate(6deg)',
            fontFamily:'"Space Grotesk", sans-serif', fontSize:10, fontWeight:700,
            color:tok.stampRed, letterSpacing:2,
          }}>HELD</div>
        )}
      </div>

      <div style={{
        padding:'14px 24px', display:'grid', gridTemplateColumns:'96px 1fr',
        rowGap:6, columnGap:12, borderTop:`0.5px dashed ${tok.rule2}`, borderBottom:`0.5px dashed ${tok.rule2}`,
        fontFamily:'"Space Grotesk", sans-serif', fontSize:10.5, letterSpacing:1, color:tok.mute,
      }}>
        <span style={{textTransform:'uppercase'}}>Status</span>
        <span>
          <select value={task.status} onChange={e=>update(task.id,{status:e.target.value})}
            style={{ all:'unset', color:tok.ink, cursor:'pointer', fontFamily:'inherit', fontSize:11, letterSpacing:1 }}>
            {Object.keys(C_STATUS_META).map(s =>
              <option key={s} value={s} style={{background:tok.bg}}>{C_STATUS_META[s].label}</option>)}
          </select>
        </span>
        <span style={{textTransform:'uppercase'}}>Priority</span>
        <span style={{color:tok.ink, display:'flex', gap:10, alignItems:'center'}}>
          <CPriNum p={task.priority} tok={tok}/> <span style={{fontSize:11}}>P{task.priority}</span>
        </span>
        <span style={{textTransform:'uppercase'}}>Updated</span>
        <span style={{color:tok.ink}}>{SEGMENTS_UTIL.relTime(task.updated_at)}</span>
        <span style={{textTransform:'uppercase'}}>Opened</span>
        <span style={{color:tok.ink}}>{SEGMENTS_UTIL.relTime(task.created_at)}</span>
        {task.closed_at && (<>
          <span style={{textTransform:'uppercase'}}>Closed</span>
          <span style={{color:tok.ink}}>{SEGMENTS_UTIL.relTime(task.closed_at)}</span>
        </>)}
      </div>

      <div style={{
        padding:'20px 24px',
        fontFamily:'"Instrument Serif", "Times New Roman", serif',
        fontSize:15, lineHeight:1.55, color: tok.ink, whiteSpace:'pre-wrap',
      }}>
        {task.body ? (
          <>
            <span style={{
              fontSize:44, float:'left', lineHeight:0.9, marginRight:6, marginTop:6,
              fontFamily:'"Instrument Serif", serif', color: tok.ink,
            }}>{task.body.trim()[0]}</span>
            {task.body.trim().slice(1)}
          </>
        ) : <em style={{color:tok.faint}}>No description on file.</em>}
      </div>

      {(deps.length>0 || dependents.length>0) && (
        <div style={{padding:'16px 24px', borderTop:`0.5px solid ${tok.rule}`}}>
          {deps.length>0 && <>
            <div style={{
              fontFamily:'"Space Grotesk", sans-serif', fontSize:9.5, letterSpacing:2,
              color:tok.mute, textTransform:'uppercase', marginBottom:8,
            }}>Held by</div>
            {deps.map(d=>(
              <div key={d.id} style={{
                padding:'6px 0', borderBottom:`0.5px dashed ${tok.rule2}`,
                display:'grid', gridTemplateColumns:'110px 1fr auto', gap:10, alignItems:'center',
              }}>
                <CStatusTag status={d.status} tok={tok}/>
                <span style={{
                  fontFamily:'"Instrument Serif", serif', fontSize:14, color:tok.ink,
                  overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                }}>{d.title}</span>
                <span style={{fontFamily:'"Space Grotesk", sans-serif', fontSize:9, color:tok.faint}}>{d.id}</span>
              </div>
            ))}
          </>}
          {dependents.length>0 && <div style={{marginTop:14}}>
            <div style={{
              fontFamily:'"Space Grotesk", sans-serif', fontSize:9.5, letterSpacing:2,
              color:tok.mute, textTransform:'uppercase', marginBottom:8,
            }}>Holds up</div>
            {dependents.map(d=>(
              <div key={d.id} style={{
                padding:'6px 0', borderBottom:`0.5px dashed ${tok.rule2}`,
                display:'grid', gridTemplateColumns:'110px 1fr auto', gap:10, alignItems:'center',
              }}>
                <CStatusTag status={d.status} tok={tok}/>
                <span style={{
                  fontFamily:'"Instrument Serif", serif', fontSize:14, color:tok.ink,
                  overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                }}>{d.title}</span>
                <span style={{fontFamily:'"Space Grotesk", sans-serif', fontSize:9, color:tok.faint}}>{d.id}</span>
              </div>
            ))}
          </div>}
        </div>
      )}
    </aside>
  );
}

function CPalette({ tok, open, onClose, tasks, onJump, setView, theme, setTheme, update, selectedId }) {
  const [q, setQ] = useStateC('');
  const [idx, setIdx] = useStateC(0);
  const ref = useRefC(null);
  useEffectC(()=>{ if(open){setQ(''); setIdx(0); setTimeout(()=>ref.current?.focus(),10);} }, [open]);
  const items = useMemoC(() => {
    const base = [
      {type:'view', label:'Go to List', run:()=>setView('list')},
      {type:'view', label:'Go to Board', run:()=>setView('kanban')},
      {type:'view', label:'Go to Lineage', run:()=>setView('graph')},
      {type:'pref', label:`Theme · ${theme==='dark'?'Light':'Dark'}`, run:()=>setTheme(theme==='dark'?'light':'dark')},
      ...(selectedId ? [
        {type:'act', label:`Settle ${selectedId}`, run:()=>update(selectedId,{status:'done', closed_at:new Date().toISOString()})},
        {type:'act', label:`Claim ${selectedId}`, run:()=>update(selectedId,{status:'in_progress'})},
      ]:[]),
      ...tasks.slice(0,40).map(t=>({type:'task', label: t.title, sub: t.id, run:()=>onJump(t.id)})),
    ];
    if (!q) return base;
    const nq = q.toLowerCase();
    return base.filter(i => i.label.toLowerCase().includes(nq) || (i.sub||'').toLowerCase().includes(nq));
  }, [q, tasks, theme, selectedId]);
  if (!open) return null;
  return (
    <div onClick={onClose} style={{
      position:'absolute', inset:0, background:'rgba(20,17,14,0.55)', zIndex:100,
      display:'flex', justifyContent:'center', alignItems:'flex-start', paddingTop:100,
    }}>
      <div onClick={e=>e.stopPropagation()} style={{
        width:600, background:tok.bg, border:`2px solid ${tok.rule}`,
        boxShadow:'12px 12px 0 rgba(22,19,18,0.2)',
      }}>
        <div style={{ padding:'16px 20px 8px', borderBottom:`0.5px solid ${tok.rule2}` }}>
          <div style={{
            fontFamily:'"Space Grotesk", sans-serif', fontSize:9, letterSpacing:3,
            color:tok.mute, textTransform:'uppercase', marginBottom:2,
          }}>Command</div>
          <input ref={ref} value={q} onChange={e=>{setQ(e.target.value); setIdx(0);}}
            onKeyDown={e=>{
              if(e.key==='Escape') onClose();
              else if(e.key==='ArrowDown'){setIdx(i=>Math.min(items.length-1,i+1)); e.preventDefault();}
              else if(e.key==='ArrowUp'){setIdx(i=>Math.max(0,i-1)); e.preventDefault();}
              else if(e.key==='Enter'){items[idx]?.run(); onClose(); e.preventDefault();}
            }}
            placeholder="what shall we do?"
            style={{ all:'unset', width:'100%',
              fontFamily:'"Instrument Serif", serif', fontSize:22, color:tok.ink, letterSpacing:-0.2,
            }}/>
        </div>
        <div style={{maxHeight:380, overflow:'auto'}}>
          {items.slice(0,24).map((it,i) => (
            <div key={i} onMouseEnter={()=>setIdx(i)} onClick={()=>{it.run(); onClose();}}
              style={{
                padding:'10px 20px',
                display:'flex', justifyContent:'space-between', alignItems:'baseline',
                cursor:'pointer',
                background: idx===i ? tok.paper : 'transparent',
                borderLeft:`3px solid ${idx===i ? tok.stampRed : 'transparent'}`,
                borderBottom:`0.5px dashed ${tok.rule2}`,
              }}>
              <div>
                <span style={{
                  fontFamily:'"Space Grotesk", sans-serif', fontSize:9, letterSpacing:2,
                  color: tok.mute, textTransform:'uppercase', marginRight:10,
                }}>{it.type}</span>
                <span style={{ fontFamily:'"Instrument Serif", serif', fontSize:15, color:tok.ink }}>{it.label}</span>
              </div>
              {it.sub && <span style={{fontFamily:'"Space Grotesk", sans-serif', fontSize:9.5, color:tok.faint, letterSpacing:1}}>{it.sub}</span>}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function CComposer({ tok, open, onClose, onCreate }) {
  const [title, setTitle] = useStateC('');
  const [body, setBody] = useStateC('');
  const [pri, setPri] = useStateC(1);
  const ref = useRefC(null);
  useEffectC(()=>{ if(open){setTitle(''); setBody(''); setPri(1); setTimeout(()=>ref.current?.focus(),10);}}, [open]);
  if (!open) return null;
  const submit = () => {
    if (!title.trim()) return;
    const id = 'SEG-'+(500+Math.floor(Math.random()*500));
    onCreate({
      id, project:'proj_segments', title:title.trim(), body:body.trim(),
      priority:pri, status:'todo', blocked_by:[],
      created_at:new Date().toISOString(), updated_at:new Date().toISOString(),
      closed_at:null, sort_order:0,
    });
    onClose();
  };
  return (
    <div style={{
      borderBottom:`1px solid ${tok.rule}`, background:tok.paper, padding:'18px 24px',
    }}>
      <div style={{ fontFamily:'"Space Grotesk", sans-serif', fontSize:9, letterSpacing:3, color:tok.mute, textTransform:'uppercase', marginBottom:6 }}>
        New Entry
      </div>
      <input ref={ref} value={title} onChange={e=>setTitle(e.target.value)}
        onKeyDown={e=>{
          if(e.key==='Enter' && !e.shiftKey) submit();
          if(e.key==='Escape') onClose();
        }}
        placeholder="subject of the work…"
        style={{
          all:'unset', width:'100%',
          fontFamily:'"Instrument Serif", serif', fontSize:22, color:tok.ink, letterSpacing:-0.2,
        }}/>
      <textarea value={body} onChange={e=>setBody(e.target.value)}
        placeholder="notes (optional)" rows={2}
        style={{
          width:'100%', boxSizing:'border-box', background:'transparent', border:'none', outline:'none',
          fontFamily:'"Instrument Serif", serif', fontSize:14, color:tok.mute, resize:'none',
          marginTop:6, padding:0,
        }}/>
      <div style={{ display:'flex', gap:10, alignItems:'center', marginTop:6,
        fontFamily:'"Space Grotesk", sans-serif', fontSize:10, letterSpacing:1, color:tok.mute, textTransform:'uppercase',
      }}>
        <span>Priority</span>
        {[0,1,2,3].map(p => (
          <button key={p} onClick={()=>setPri(p)} style={{
            all:'unset', cursor:'pointer', padding:'2px 8px',
            border:`1px solid ${pri===p ? tok.ink : tok.rule2}`,
            color: pri===p ? tok.ink : tok.mute,
            fontFamily:'"Instrument Serif", serif', fontSize:13, fontStyle:'italic',
          }}>{['I','II','III','IV'][p]}</button>
        ))}
        <span style={{flex:1}}/>
        <span>↵ submit · esc discard</span>
      </div>
    </div>
  );
}

function SegmentsC() {
  const [theme, setTheme] = useStateC('light');
  const tok = C_TOK[theme];
  const [view, setView] = useStateC('list');
  const [search, setSearch] = useStateC('');
  const [selectedId, setSelectedId] = useStateC('SEG-110');
  const [activeProject, setActiveProject] = useStateC(null);
  const [paletteOpen, setPaletteOpen] = useStateC(false);
  const [composerOpen, setComposerOpen] = useStateC(false);
  const [agentOn, setAgentOn] = useStateC(true);

  const { tasks, update, add, pulse, entering } = useTasks(SEGMENTS_SEED);
  useAgentSim({ tasks, update, add, enabled: agentOn });

  const tasksById = useMemoC(()=>Object.fromEntries(tasks.map(t=>[t.id,t])), [tasks]);
  const tasksByProject = useMemoC(()=>{
    const m={}; SEGMENTS_SEED.projects.forEach(p=>m[p.id]=[]);
    tasks.forEach(t=>(m[t.project]=m[t.project]||[]).push(t));
    return m;
  }, [tasks]);

  const filtered = useMemoC(() => {
    let o = tasks;
    if (activeProject) o = o.filter(t=>t.project===activeProject);
    if (search) o = SEGMENTS_UTIL.fuzzy(search, o);
    return o;
  }, [tasks, activeProject, search]);

  const counts = useMemoC(() => ({
    ...SEGMENTS_UTIL.counts(tasks), total: tasks.length,
    ready: SEGMENTS_UTIL.ready(tasks).length,
  }), [tasks]);

  const readyCount = counts.ready;

  useEffectC(() => {
    let gPending=false, gT;
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
        clearTimeout(gT); gPending=false;
        if (e.key==='k') setView('kanban');
        else if (e.key==='l') setView('list');
        else if (e.key==='g') setView('graph');
        return;
      }
      if (e.key==='g') { gPending=true; gT=setTimeout(()=>gPending=false, 800); return; }
      if (e.key==='j' || e.key==='k') {
        const idx = filtered.findIndex(t=>t.id===selectedId);
        const next = e.key==='j' ? Math.min(filtered.length-1, idx+1) : Math.max(0, idx-1);
        if (filtered[next]) setSelectedId(filtered[next].id);
        e.preventDefault();
        return;
      }
      if (['1','2','3','4'].includes(e.key) && selectedId) update(selectedId, {priority:parseInt(e.key)-1});
      if (e.key==='t' && selectedId) {
        const order = ['todo','in_progress','blocker','done','closed'];
        const cur = tasksById[selectedId]?.status;
        update(selectedId, {status:order[(order.indexOf(cur)+1)%order.length]});
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [paletteOpen, composerOpen, selectedId, filtered, tasksById, update]);

  return (
    <div style={{
      height:'100vh', width:'100vw', background: tok.bg, color: tok.ink,
      fontFamily:'"Space Grotesk", system-ui, sans-serif',
      display:'flex', flexDirection:'column', overflow:'hidden',
    }}>
      <CTop tok={tok} theme={theme} setTheme={setTheme} view={view} setView={setView}
        search={search} setSearch={setSearch}
        onPalette={()=>setPaletteOpen(true)} agentOn={agentOn} setAgentOn={setAgentOn}
        counts={counts}/>
      <div style={{ display:'flex', flex:1, minHeight:0 }}>
        <CSide tok={tok} projects={SEGMENTS_SEED.projects} tasksByProject={tasksByProject}
          activeProject={activeProject} setActiveProject={setActiveProject} readyCount={readyCount}/>
        <div style={{ flex:1, display:'flex', flexDirection:'column', minWidth:0 }}>
          <CComposer tok={tok} open={composerOpen} onClose={()=>setComposerOpen(false)} onCreate={add}/>
          {view==='list'   && <CListView tok={tok} tasks={filtered} tasksById={tasksById} selectedId={selectedId} setSelectedId={setSelectedId} pulse={pulse} entering={entering}/>}
          {view==='kanban' && <CKanban tok={tok} tasks={filtered} tasksById={tasksById} selectedId={selectedId} setSelectedId={setSelectedId} update={update} pulse={pulse}/>}
          {view==='graph'  && <CGraph  tok={tok} tasks={filtered} tasksById={tasksById} selectedId={selectedId} setSelectedId={setSelectedId}/>}
        </div>
        {selectedId && tasksById[selectedId] && (
          <CDetail tok={tok} task={tasksById[selectedId]} tasksById={tasksById} onClose={()=>setSelectedId(null)} update={update}/>
        )}
      </div>
      <CPalette tok={tok} open={paletteOpen} onClose={()=>setPaletteOpen(false)}
        tasks={tasks} onJump={setSelectedId} setView={setView}
        theme={theme} setTheme={setTheme} update={update} selectedId={selectedId}/>
    </div>
  );
}

window.SegmentsC = SegmentsC;
