/* Pure functions (node-testable): this file is NOT bundled. The
   build transforms it on its own and inlines the result between
   the "lp pure" marker comments in the page, where these
   declarations become page globals shared by every bundle -- the
   same arrangement the pre-modular page had. Keep it free of
   imports, exports and outside references: the node suites lift
   this code out of the built page and eval it standalone. */
/* Launchpad grid dimensions for a viewport: columns 5-8, rows
   3-6, following the viewport so a small desktop window still
   paginates instead of clipping. */
function lpDims(w,h){
 const cols=Math.max(5,Math.min(8,Math.round(w/175)));
 const rows=Math.max(3,Math.min(6,Math.floor(h/175)));
 return {cols,rows,per:cols*rows};
}
function lpPageCount(count,w,h){ return Math.max(1,Math.ceil(count/lpDims(w,h).per)); }
function lpPageOfIndex(idx,w,h){ return Math.floor(idx/lpDims(w,h).per); }
/* How many of `centers` (row-major visual order, {x,y} centres)
   rank before the pointer in row-major reading order: an item
   ranks before when its row is above the pointer's row, or it
   shares the pointer's row and sits left of it. The pointer's
   row is the painted row nearest to it vertically. */
function lpInsertIndex(centers,px,py){
 if(!centers||!centers.length) return 0;
 const rowsY=[];
 for(const c of centers){ if(!rowsY.some(y=>Math.abs(y-c.y)<3)) rowsY.push(c.y); }
 rowsY.sort((a,b)=>a-b);
 let pr=0,bd=Infinity;
 rowsY.forEach((y,i)=>{ const d=Math.abs(y-py); if(d<bd){ bd=d; pr=i; } });
 let n=0;
 for(const c of centers){
  const ri=rowsY.findIndex(y=>Math.abs(y-c.y)<3);
  if(ri<pr||(ri===pr&&c.x<px)) n++;
 }
 return n;
}
/* Launchpad's own name filter (distinct from Spotlight): the
   keys of entries whose name contains the query, in entry
   order; an empty query keeps everything. */
function lpFilterKeys(entries,q){
 const s=(q||'').trim().toLowerCase();
 if(!s) return (entries||[]).map(e=>e.key);
 return (entries||[]).filter(e=>e.name.toLowerCase().indexOf(s)>=0).map(e=>e.key);
}
