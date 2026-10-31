/* Pure functions (node-testable): this file is NOT bundled. The
   build transforms it on its own and inlines the result between
   the "events pure" marker comments in the page, where these
   declarations become page globals shared by every bundle -- the
   same arrangement the pre-modular page had. Keep it free of
   imports, exports and outside references: the node suites lift
   this code out of the built page and eval it standalone. */
/* v0.59 block 3: the change-feed client core. The daemon's event
   bus (events.go) streams two message families over
   /api/view/stream: view commands ({"cmd": ...}, the F8/Spotlight
   channel that predates the bus) and bus events
   ({"topic": ..., "id": ...}). This hub routes both: topic
   subscribers for state changes, command subscribers for view
   commands. Events with the same topic+id inside the dedupe
   window are delivered once -- a rename and the datastore save
   it rides on can legitimately announce the same change twice,
   and surfaces re-pull state anyway, so a duplicate costs a
   redundant render at worst. */
/* Reconnect backoff for the stream: 1s, 2s, 4s, ... capped at
   15s. The attempt counter resets when a connection opens. */
function evBackoffDelay(attempt){
 const a=Math.max(0,attempt|0);
 return Math.min(1000*Math.pow(2,a),15000);
}
function evDedupeKey(topic,id){ return topic+'|'+(id||''); }
/* createEvHub(nowFn): nowFn is injectable for tests; the page
   uses the default wall clock. dispatch(rawJson) returns what
   happened: 'event' | 'cmd' | 'dup' | 'bad' | 'ignored'. */
function createEvHub(nowFn?: () => number){
 const now: () => number=(typeof nowFn==='function')?nowFn:function(){ return Date.now(); };
 const subs: Map<string, Set<(ev: any) => void>>=new Map(); /* topic -> Set<fn> */
 const cmdSubs: Set<(m: any) => void>=new Set();
 const seen: Map<string, number>=new Map(); /* dedupe key -> timestamp (insertion ordered) */
 const DEDUPE_MS=1500, SEEN_CAP=200;
 function on(topic,fn){
  let set=subs.get(topic); if(!set){ set=new Set(); subs.set(topic,set); }
  set.add(fn);
  return function(){ set.delete(fn); };
 }
 function onCmd(fn){ cmdSubs.add(fn); return function(){ cmdSubs.delete(fn); }; }
 function markSeen(key){
  seen.set(key,now());
  if(seen.size>SEEN_CAP){ const k=seen.keys().next().value; seen.delete(k); }
 }
 function dispatch(raw){
  let m=null;
  try{ m=JSON.parse(raw); }catch(e){ return 'bad'; }
  if(!m||typeof m!=='object') return 'bad';
  if(typeof m.cmd==='string'&&m.cmd){
   cmdSubs.forEach(fn=>{ try{ fn(m); }catch(e){} });
   return 'cmd';
  }
  if(typeof m.topic==='string'&&m.topic){
   if(m.id){
    const key=evDedupeKey(m.topic,m.id);
    const t=seen.get(key);
    if(t!==undefined&&(now()-t)<DEDUPE_MS) return 'dup';
    markSeen(key);
   }
   const set=subs.get(m.topic);
   if(set) set.forEach(fn=>{ try{ fn(m); }catch(e){} });
   return 'event';
  }
  return 'ignored';
 }
 return { on:on, onCmd:onCmd, dispatch:dispatch,
  subscriberCount:function(topic){ const s=subs.get(topic); return s?s.size:0; } };
}
