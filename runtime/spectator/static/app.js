(() => {
'use strict';
const ACTIVE=new Set(['queued','starting','running','stopping']);
const state={session:null,live:[],recent:[],selected:'',follow:true,connected:false,generation:0,frameURL:'',frameSeq:'',frameAt:0,lastLatency:0,lastLatencyID:''};
const ids=['connection-dot','connection-label','unavailable','empty','watch','session-title','session-subtitle','status','follow-live','frame-status','game-frame','frame-placeholder','ready-state','tetris-board','current-piece','next-piece','score','lines','level','moves','elapsed','frame','planner-activity','placement','latency','model','live-list','recent-list'];
const e={};
document.addEventListener('DOMContentLoaded',()=>{ids.forEach(id=>e[id]=document.getElementById(id));buildBoard();e['follow-live'].onclick=followLive;const g=++state.generation;refresh().catch(handleError);poll(g,refresh,750);poll(g,refreshFrame,33);});

async function refresh(){
  const query=state.selected?`?session=${encodeURIComponent(state.selected)}`:'';
  const r=await fetch(`/v1/watch${query}`,{cache:'no-store',credentials:'same-origin'});
  if(r.status===503)throw unavailableError();
  if(r.status===404&&state.selected){state.follow=true;state.selected='';return refresh();}
  if(!r.ok)throw new Error(`spectator ${r.status}`);
  const data=await r.json();
  state.connected=true;
  state.session=data?.session||null;
  state.live=Array.isArray(data?.live)?data.live:[];
  state.recent=Array.isArray(data?.recent)?data.recent:[];
  if(state.follow&&state.session&&!ACTIVE.has(state.session.status)){
    const next=state.live.find(x=>ACTIVE.has(x.status)&&x.id!==state.session.id);
    state.selected=next?next.id:'';
    if(state.selected)return refresh();
  }
  if(state.follow&&state.session&&ACTIVE.has(state.session.status))state.selected=state.session.id;
  render();
}

function render(){
  connection(true);
  e.unavailable.classList.add('hidden');
  renderList(e['live-list'],state.live,'No live sessions yet.');
  renderRecent();
  const x=state.session;
  if(!x){e.watch.classList.add('hidden');e.empty.classList.remove('hidden');clearFrame();return;}
  e.empty.classList.add('hidden');e.watch.classList.remove('hidden');
  e['follow-live'].classList.toggle('hidden',state.follow);
  e.status.className=`status-pill ${x.status||''}`;e.status.textContent=x.status||'unknown';
  const planner=x.planner||'planner',profile=x.profile||'session';
  e['session-title'].textContent=`${title(profile)} · ${planner}`;
  e['session-subtitle'].textContent=[short(x.id),x.model_label&&`model ${x.model_label}`,ACTIVE.has(x.status)?'live feed':'completed session'].filter(Boolean).join(' · ');
  const boxxle=x.profile==='boxxle';
  const t=x.tetris;
  const b=x.boxxle;
  e['tetris-board'].classList.toggle('hidden', boxxle);
  if(e['boxxle-board'])e['boxxle-board'].classList.toggle('hidden', !boxxle);
  const pieces=e['current-piece']?.closest('.pieces');
  if(pieces)pieces.classList.toggle('hidden', boxxle);
  if(boxxle)warehouse(b?.grid); else board(t?.board);
  e['current-piece'].textContent=piece(t?.current_piece);
  e['next-piece'].textContent=piece(t?.next_piece);
  label(e.score,boxxle,'Set','Score');label(e.lines,boxxle,'Solved','Lines');label(e.level,boxxle,'Set','Level');
  e.score.textContent=boxxle?value(b?.set):value(t?.score);
  e.lines.textContent=boxxle?(b?.solved?'yes':'no'):value(t?.lines);
  e.level.textContent=boxxle?value(b?.set):value(t?.level);
  e.moves.textContent=value(x.moves);e.elapsed.textContent=duration(x.elapsed_seconds);e.frame.textContent=value(x.frame);
  e['ready-state'].textContent=boxxle?(b?.solved?'Solved':b?.ready?'Ready':'In motion'):(t?(t.game_over?'Game over':t.ready?'Ready':'In motion'):'State unavailable');
  e['planner-activity'].textContent=x.planner_activity==='planning'?'Planning…':'Idle';
  label(e.placement,boxxle,'Latest move','Latest placement');
  e.placement.textContent=boxxle?move(x.latest_move):placement(x.latest_placement);
  e.latency.textContent=latestLatency(x.id,x.planner_latency_ms);
  e.model.textContent=x.model_label||'—';
  if(!x.frame_available)placeholder('Waiting for frame');
}

function renderRecent(){renderList(e['recent-list'],state.recent,'No completed sessions yet.');}
function renderList(node,items,empty){
  if(!node)return;
  node.replaceChildren();
  if(!items.length){const p=document.createElement('p');p.className='muted';p.textContent=empty;node.append(p);return;}
  items.forEach(x=>{
    const b=document.createElement('button');b.type='button';b.className='recent-item'+(x.id===state.selected?' selected':'');
    const a=document.createElement('strong'),m=document.createElement('span'),n=document.createElement('span');
    a.textContent=`${x.planner||'planner'} · ${short(x.id)}`;m.textContent=`${x.status||'done'} · ${x.moves??0} moves · ${duration(x.elapsed_seconds)}`;n.textContent=x.tetris?`score ${x.tetris.score??0} · ${x.tetris.lines??0} lines`:x.boxxle?`set ${x.boxxle.set??0} · ${x.boxxle.solved?'solved':'playing'}`:'summary available';
    b.append(a,m,n);b.onclick=()=>select(x.id);node.append(b);
  });
}

function select(id){if(!id)return;state.follow=false;state.selected=id;state.frameSeq='';state.frameAt=0;clearFrame();refresh().catch(handleError);}
function followLive(){state.follow=true;state.selected='';state.frameSeq='';state.frameAt=0;clearFrame();refresh().catch(handleError);}

async function refreshFrame(){
  const x=state.session;if(!state.connected||!x?.frame_available||!x.id)return;
  const r=await fetch(`/v1/frame/${encodeURIComponent(x.id)}`,{cache:'no-store',credentials:'same-origin'});
  if(r.status===404)return placeholder('Frame unavailable');
  if(r.status===503)throw unavailableError();
  if(!r.ok)throw new Error(`frame ${r.status}`);
  const seq=r.headers.get('X-GamePilot-Sequence')||'';
  if(seq&&seq===state.frameSeq)return freshness();
  const blob=await r.blob();if(!blob.size)return;
  const url=URL.createObjectURL(blob);if(state.frameURL)URL.revokeObjectURL(state.frameURL);state.frameURL=url;state.frameSeq=seq;state.frameAt=Date.now();
  e['game-frame'].src=url;e['game-frame'].classList.remove('hidden');e['frame-placeholder'].classList.add('hidden');e['frame-status'].textContent=seq?`Live · seq ${seq}`:'Live';
}
function freshness(){if(!state.frameAt)return;const stale=Date.now()-state.frameAt>3000&&ACTIVE.has(state.session?.status);if(stale)e['frame-status'].textContent='Frame stale';}
function placeholder(text){e['game-frame'].classList.add('hidden');e['frame-placeholder'].classList.remove('hidden');e['frame-placeholder'].textContent=text;e['frame-status'].textContent=text;}
function clearFrame(){if(state.frameURL)URL.revokeObjectURL(state.frameURL);state.frameURL='';state.frameSeq='';if(e['game-frame']){e['game-frame'].removeAttribute('src');e['game-frame'].classList.add('hidden');}if(e['frame-placeholder'])placeholder('No frame published yet');}

function buildBoard(){
  const f=document.createDocumentFragment();for(let i=0;i<180;i++){const n=document.createElement('span');n.className='cell';f.append(n);}e['tetris-board'].append(f);
  if(!e['boxxle-board']&&e['tetris-board']?.parentElement){
    const board=document.createElement('div');board.id='boxxle-board';board.className='boxxle-board hidden';board.setAttribute('aria-label','Boxxle warehouse');
    const cells=document.createDocumentFragment();for(let i=0;i<90;i++){const n=document.createElement('span');n.className='cell';cells.append(n);}board.append(cells);
    e['tetris-board'].after(board);e['boxxle-board']=board;
  }
}
function board(data){const cells=e['tetris-board'].children;for(let r=0;r<18;r++)for(let c=0;c<10;c++){const cell=cells[r*10+c];if(cell)cell.className=Number(data?.[r]?.[c])===1?'cell filled':'cell';}}
function warehouse(data){const cells=e['boxxle-board']?.children;if(!cells)return;const kind={ '#':'wall','$':'box','.':'goal','*':'box-goal','@':'player','+':'player-goal'};for(let r=0;r<9;r++)for(let c=0;c<10;c++){const cell=cells[r*10+c];if(!cell)continue;const ch=data?.[r]?.[c]||' ';cell.className='cell'+(kind[ch]?' '+kind[ch]:'');}}
function piece(p){return p?.kind?`${p.kind}${Number.isInteger(p.rotation)?` r${p.rotation}`:''}`:'—';}
function placement(p){return p&&Number.isInteger(p.rotation)&&Number.isInteger(p.target_column)?`r${p.rotation} → col ${p.target_column}`:'—';}
function move(p){return p?.direction||'—';}
function label(node,boxxle,boxxleText,tetrisText){const span=node?.previousElementSibling;if(span)span.textContent=boxxle?boxxleText:tetrisText;}
function latestLatency(id,ms){if(id&&id!==state.lastLatencyID){state.lastLatency=0;state.lastLatencyID=id;}if(ms){state.lastLatency=ms;state.lastLatencyID=id||state.lastLatencyID;}return state.lastLatency?`${state.lastLatency} ms`:'—';}
function value(v){return v===0||v?String(v):'—';}
function duration(sec){sec=Math.max(0,Number(sec)||0);const m=Math.floor(sec/60),s=Math.floor(sec%60);return m?`${m}m ${String(s).padStart(2,'0')}s`:`${s}s`;}
function short(id){return id?String(id).slice(0,8):'—';}
function title(v){return String(v||'session').replace(/(^|[-_ ])([a-z])/g,(_,a,b)=>a+b.toUpperCase());}
function connection(ok){state.connected=ok;e['connection-dot'].classList.toggle('live',ok);e['connection-dot'].classList.toggle('error',!ok);e['connection-label'].textContent=ok?'Public feed live':'Feed unavailable';}
function unavailableError(){const err=new Error('spectator unavailable');err.unavailable=true;return err;}
function handleError(err){state.connected=false;connection(false);if(err?.unavailable){e.watch.classList.add('hidden');e.empty.classList.add('hidden');e.unavailable.classList.remove('hidden');}else if(!state.session){e.watch.classList.add('hidden');e.empty.classList.remove('hidden');}}
async function poll(g,fn,ms){while(g===state.generation){const started=Date.now();try{await fn();if(fn===refreshFrame)freshness();}catch(err){handleError(err);}const wait=ms-(Date.now()-started);if(wait>0)await new Promise(resolve=>setTimeout(resolve,wait));}}
})();
