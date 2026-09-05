(() => {
'use strict';
const ACTIVE=new Set(['queued','starting','running','stopping']);
const state={session:null,recent:[],selected:'',follow:true,connected:false,generation:0,frameURL:'',frameSeq:'',frameAt:0};
const ids=['connection-dot','connection-label','unavailable','empty','watch','session-title','session-subtitle','status','follow-live','frame-status','game-frame','frame-placeholder','ready-state','tetris-board','current-piece','next-piece','score','lines','level','moves','elapsed','frame','planner-activity','placement','latency','model','recent-list'];
const e={};
document.addEventListener('DOMContentLoaded',()=>{ids.forEach(id=>e[id]=document.getElementById(id));buildBoard();e['follow-live'].onclick=followLive;const g=++state.generation;refresh().catch(handleError);poll(g,refresh,750);poll(g,refreshFrame,220);});

async function refresh(){
  const query=!state.follow&&state.selected?`?session=${encodeURIComponent(state.selected)}`:'';
  const r=await fetch(`/v1/watch${query}`,{cache:'no-store',credentials:'same-origin'});
  if(r.status===503)throw unavailableError();
  if(r.status===404&&state.selected){state.follow=true;state.selected='';return refresh();}
  if(!r.ok)throw new Error(`spectator ${r.status}`);
  const data=await r.json();
  state.connected=true;
  state.session=data?.session||null;
  state.recent=Array.isArray(data?.recent)?data.recent:[];
  if(state.follow&&state.session)state.selected=state.session.id;
  render();
}

function render(){
  connection(true);
  e.unavailable.classList.add('hidden');
  renderRecent();
  const x=state.session;
  if(!x){e.watch.classList.add('hidden');e.empty.classList.remove('hidden');clearFrame();return;}
  e.empty.classList.add('hidden');e.watch.classList.remove('hidden');
  e['follow-live'].classList.toggle('hidden',state.follow);
  e.status.className=`status-pill ${x.status||''}`;e.status.textContent=x.status||'unknown';
  const planner=x.planner||'planner',profile=x.profile||'session';
  e['session-title'].textContent=`${title(profile)} · ${planner}`;
  e['session-subtitle'].textContent=[short(x.id),x.model_label&&`model ${x.model_label}`,ACTIVE.has(x.status)?'live feed':'completed session'].filter(Boolean).join(' · ');
  const t=x.tetris;
  board(t?.board);
  e['current-piece'].textContent=piece(t?.current_piece);
  e['next-piece'].textContent=piece(t?.next_piece);
  e.score.textContent=value(t?.score);e.lines.textContent=value(t?.lines);e.level.textContent=value(t?.level);e.moves.textContent=value(x.moves);e.elapsed.textContent=duration(x.elapsed_seconds);e.frame.textContent=value(x.frame);
  e['ready-state'].textContent=t?(t.game_over?'Game over':t.ready?'Ready':'In motion'):'State unavailable';
  e['planner-activity'].textContent=x.planner_activity==='planning'?'Planning…':'Idle';
  e.placement.textContent=placement(x.latest_placement);
  e.latency.textContent=x.planner_latency_ms?`${x.planner_latency_ms} ms`:'—';
  e.model.textContent=x.model_label||'—';
  if(!x.frame_available)placeholder('Waiting for frame');
}

function renderRecent(){
  e['recent-list'].replaceChildren();
  if(!state.recent.length){const p=document.createElement('p');p.className='muted';p.textContent='No completed sessions yet.';e['recent-list'].append(p);return;}
  state.recent.forEach(x=>{
    const b=document.createElement('button');b.type='button';b.className='recent-item'+(!state.follow&&x.id===state.selected?' selected':'');
    const a=document.createElement('strong'),m=document.createElement('span'),n=document.createElement('span');
    a.textContent=`${x.planner||'planner'} · ${short(x.id)}`;m.textContent=`${x.status||'done'} · ${x.moves??0} moves · ${duration(x.elapsed_seconds)}`;n.textContent=x.tetris?`score ${x.tetris.score??0} · ${x.tetris.lines??0} lines`:'summary available';
    b.append(a,m,n);b.onclick=()=>select(x.id);e['recent-list'].append(b);
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

function buildBoard(){const f=document.createDocumentFragment();for(let i=0;i<180;i++){const n=document.createElement('span');n.className='cell';f.append(n);}e['tetris-board'].append(f);}
function board(data){const cells=e['tetris-board'].children;for(let r=0;r<18;r++)for(let c=0;c<10;c++){const cell=cells[r*10+c];cell.className=Number(data?.[r]?.[c])===1?'cell filled':'cell';}}
function piece(p){return p?.kind?`${p.kind}${Number.isInteger(p.rotation)?` r${p.rotation}`:''}`:'—';}
function placement(p){return p&&Number.isInteger(p.rotation)&&Number.isInteger(p.target_column)?`r${p.rotation} → col ${p.target_column}`:'—';}
function value(v){return v===0||v?String(v):'—';}
function duration(sec){sec=Math.max(0,Number(sec)||0);const m=Math.floor(sec/60),s=Math.floor(sec%60);return m?`${m}m ${String(s).padStart(2,'0')}s`:`${s}s`;}
function short(id){return id?String(id).slice(0,8):'—';}
function title(v){return String(v||'session').replace(/(^|[-_ ])([a-z])/g,(_,a,b)=>a+b.toUpperCase());}
function connection(ok){state.connected=ok;e['connection-dot'].classList.toggle('live',ok);e['connection-dot'].classList.toggle('error',!ok);e['connection-label'].textContent=ok?'Public feed live':'Feed unavailable';}
function unavailableError(){const err=new Error('spectator unavailable');err.unavailable=true;return err;}
function handleError(err){state.connected=false;connection(false);if(err?.unavailable){e.watch.classList.add('hidden');e.empty.classList.add('hidden');e.unavailable.classList.remove('hidden');}else if(!state.session){e.watch.classList.add('hidden');e.empty.classList.remove('hidden');}}
async function poll(g,fn,ms){while(g===state.generation){try{await fn();if(fn===refreshFrame)freshness();}catch(err){handleError(err);}await new Promise(resolve=>setTimeout(resolve,ms));}}
})();
