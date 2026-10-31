/* Pure functions (node-testable): this file is NOT bundled. The
   build transforms it on its own and inlines the result between
   the "ncgroup pure" marker comments in the page, where these
   declarations become page globals shared by every bundle -- the
   same arrangement the pre-modular page had. Keep it free of
   imports, exports and outside references: the node suites lift
   this code out of the built page and eval it standalone. */
/* v0.54: events carry a category -- 'system' for the six
   built-in sources, 'app' for events about a user-installed app
   (which also carry appId/appName). Anything without a category
   (recorded before v0.54) reads as system. */
function ncEventCategory(e){ return (e&&e.category==='app')?'app':'system'; }
/* The stack a event belongs to: system events stack by source;
   app events stack per app (key 'app:<appId>'). */
function ncGroupKey(e){ return ncEventCategory(e)==='app' ? 'app:'+((e&&e.appId)||'') : ((e&&e.source)||''); }
/* Stack recorded events (the manual's grouping): groups are
   ordered by their newest event, items inside a group stay
   newest-first (the daemon lists newest-first). A group carries
   its key, its category, and (for app groups) the app's name. */
function ncGroupEvents(events){
 const groups=[], byKey={};
 for(const e of events||[]){
  const key=ncGroupKey(e);
  let g=byKey[key];
  if(!g){ g={source:e.source,key,category:ncEventCategory(e),appId:e.appId||'',name:e.appName||'',items:[]}; byKey[key]=g; groups.push(g); }
  g.items.push(e);
 }
 return groups;
}
/* Widget layout from the nc_widgets setting string: an array of
   {id, show} in display order. Anything malformed falls back to
   the default layout rather than hiding the widget area. */
function ncLayoutWidgets(raw){
 const DEF=[{id:'calendar',show:true},{id:'server',show:true},{id:'node',show:true}];
 try{
  const a=JSON.parse(raw||'');
  if(!Array.isArray(a)||!a.length) return DEF.map(w=>({...w}));
  const seen={}, out=[];
  for(const w of a){
   if(!w||!DEF.some(d=>d.id===w.id)||seen[w.id]) return DEF.map(x=>({...x}));
   seen[w.id]=1; out.push({id:w.id,show:!!w.show});
  }
  return out;
 }catch(e){ return DEF.map(w=>({...w})); }
}
