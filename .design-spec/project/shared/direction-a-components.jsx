/* Direction A — "Obsidian"
   Refined near-black, warm off-white, single cool-steel accent.
   Hairline dividers, dense-but-breathable rhythm.
*/

const A_TOK = {
  dark: {
    bg:        '#0e1012',
    surface:   '#16181b',
    surface2:  '#1d2024',
    line:      'rgba(255,255,255,0.06)',
    line2:     'rgba(255,255,255,0.10)',
    text:      '#e7e5e2',
    muted:     '#8a8a85',
    faint:     '#5b5b57',
    accent:    '#7aa7d9',   // cool steel
    accentDim: 'rgba(122,167,217,0.14)',
    todo:      '#8a8a85',
    inprog:    '#7aa7d9',
    blocker:   '#d97a7a',
    done:      '#7ac28a',
    closed:    '#4a4a47',
    glow:      '0 0 0 1px rgba(122,167,217,0.35), 0 0 20px rgba(122,167,217,0.12)',
  },
  light: {
    bg:        '#f6f5f2',
    surface:   '#ffffff',
    surface2:  '#eeece7',
    line:      'rgba(0,0,0,0.07)',
    line2:     'rgba(0,0,0,0.12)',
    text:      '#14151a',
    muted:     '#6a6a65',
    faint:     '#9a9a95',
    accent:    '#2e5a8a',
    accentDim: 'rgba(46,90,138,0.10)',
    todo:      '#8a8a85',
    inprog:    '#2e5a8a',
    blocker:   '#b04545',
    done:      '#2f7d45',
    closed:    '#aaaaa5',
    glow:      '0 0 0 1px rgba(46,90,138,0.35), 0 0 20px rgba(46,90,138,0.10)',
  }
};

const A_STATUS_META = {
  todo:        { label:'Todo',        glyph:'○' },
  in_progress: { label:'In progress', glyph:'◐' },
  blocker:     { label:'Blocker',     glyph:'◉' },
  done:        { label:'Done',        glyph:'●' },
  closed:      { label:'Closed',      glyph:'⊘' },
};

// Priority: filled horizontal bars 0..3 (0 = all 4 filled = urgent)
function APriorityBars({ p, tok }) {
  const filled = 4 - p; // p0 -> 4, p3 -> 1
  const colors = ['#d97a7a','#e0b675','#7aa7d9', tok.faint];
  const color = colors[p];
  return (
    <span aria-label={`Priority P${p}`} style={{
      display:'inline-flex', gap:2, alignItems:'flex-end', height:10, verticalAlign:'middle',
    }}>
      {[0,1,2,3].map(i => (
        <span key={i} style={{
          width:2.5, height: 4 + i*1.5,
          background: i < filled ? color : tok.line2,
          borderRadius: 0.5,
        }}/>
      ))}
    </span>
  );
}

function AStatusPill({ status, tok, pulsing }) {
  const meta = A_STATUS_META[status];
  const color = tok[status === 'in_progress' ? 'inprog' : status];
  return (
    <span
      data-status={status}
      className={pulsing ? 'seg-pulse' : ''}
      style={{
        display:'inline-flex', alignItems:'center', gap:6,
        fontSize:11.5, color, fontWeight:500, letterSpacing:0.1,
        minWidth: 92,
      }}
    >
      <span style={{ fontSize:13, lineHeight:1, width:12, textAlign:'center' }}>{meta.glyph}</span>
      <span>{meta.label}</span>
    </span>
  );
}

function ABlockerChip({ ids, tasksById, tok }) {
  if (!ids || !ids.length) return null;
  const unresolved = ids.some(id => tasksById[id] && tasksById[id].status !== 'done');
  return (
    <span style={{
      fontSize:11, color: unresolved ? tok.blocker : tok.faint,
      border: `1px solid ${unresolved ? 'rgba(217,122,122,0.25)' : tok.line2}`,
      padding:'1px 6px', borderRadius:3, fontVariantNumeric:'tabular-nums',
    }}>
      {unresolved ? '✕' : '✓'} {ids.join(', ')}
    </span>
  );
}

// ── Sparkline-ish status dots for a project
function AProjectBadge({ project, tasks, tok, active, onClick }) {
  const counts = SEGMENTS_UTIL.counts(tasks);
  const total = tasks.length;
  return (
    <button onClick={onClick} style={{
      all:'unset', cursor:'pointer',
      display:'flex', alignItems:'center', gap:10,
      padding:'7px 10px', borderRadius:5,
      background: active ? tok.surface2 : 'transparent',
      width:'100%', boxSizing:'border-box',
    }}>
      <span style={{
        width:6, height:6, borderRadius:3,
        background: active ? tok.accent : tok.faint,
      }}/>
      <span style={{ fontSize:12.5, color: active ? tok.text : tok.muted, flex:1, fontWeight: active?500:400 }}>
        {project.name}
      </span>
      <span style={{
        fontSize:10.5, color: tok.faint,
        fontVariantNumeric:'tabular-nums',
      }}>{total}</span>
      <span style={{ display:'flex', gap:1.5, alignItems:'center' }}>
        {['blocker','in_progress','todo','done'].map(s => {
          const n = counts[s];
          if (!n) return <span key={s} style={{ width:0 }}/>;
          const color = s==='in_progress'? tok.inprog : s==='done'? tok.done : s==='blocker'? tok.blocker : tok.todo;
          return <span key={s} title={`${n} ${s}`} style={{
            width:3, height: 4 + Math.min(n,8), background:color, borderRadius:0.5,
          }}/>;
        })}
      </span>
    </button>
  );
}

// ── List row
function AListRow({ task, tasksById, tok, selected, expanded, onClick, onToggleExpand, pulsing, entering }) {
  const body1 = SEGMENTS_UTIL.firstLine(task.body);
  return (
    <div
      role="row"
      aria-selected={selected}
      className={entering ? 'seg-enter' : ''}
      onClick={onClick}
      style={{
        display:'grid',
        gridTemplateColumns:'22px 92px 70px 1fr auto auto',
        gap:12, alignItems:'center',
        padding:'9px 16px',
        borderBottom:`1px solid ${tok.line}`,
        background: selected ? tok.surface2 : 'transparent',
        cursor:'pointer',
        fontSize:13,
        position:'relative',
      }}
    >
      {selected && (
        <span style={{
          position:'absolute', left:0, top:6, bottom:6, width:2, background:tok.accent, borderRadius:2,
        }}/>
      )}
      <span style={{ color: tok.faint, fontFamily:'"Geist Mono", ui-monospace, monospace', fontSize:10.5 }}>
        <APriorityBars p={task.priority} tok={tok}/>
      </span>
      <AStatusPill status={task.status} tok={tok} pulsing={pulsing}/>
      <span style={{
        color: tok.faint, fontFamily:'"Geist Mono", ui-monospace, monospace',
        fontSize:11, fontVariantNumeric:'tabular-nums',
      }}>{task.id}</span>
      <span style={{ minWidth:0, display:'flex', gap:10, alignItems:'baseline', overflow:'hidden' }}>
        <span style={{
          color: task.status==='closed'? tok.faint : tok.text, fontWeight:450,
          whiteSpace:'nowrap', overflow:'hidden', textOverflow:'ellipsis',
          flexShrink:1, minWidth:0,
        }}>
          {task.title}
        </span>
        {body1 && (
          <span style={{
            color: tok.muted, fontSize:12,
            whiteSpace:'nowrap', overflow:'hidden', textOverflow:'ellipsis',
            flex:1, minWidth:0,
          }}>— {body1}</span>
        )}
      </span>
      <ABlockerChip ids={task.blocked_by} tasksById={tasksById} tok={tok}/>
      <span style={{ color: tok.faint, fontSize:11.5, fontVariantNumeric:'tabular-nums', minWidth:48, textAlign:'right' }}>
        {SEGMENTS_UTIL.relTime(task.updated_at)}
      </span>
    </div>
  );
}

// Expose
window.A_TOK = A_TOK;
window.A_STATUS_META = A_STATUS_META;
window.APriorityBars = APriorityBars;
window.AStatusPill = AStatusPill;
window.ABlockerChip = ABlockerChip;
window.AProjectBadge = AProjectBadge;
window.AListRow = AListRow;
