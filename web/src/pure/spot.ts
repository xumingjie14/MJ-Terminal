/* Pure functions (node-testable): this file is NOT bundled. The
   build transforms it on its own and inlines the result between
   the "spot pure" marker comments in the page, where these
   declarations become page globals shared by every bundle -- the
   same arrangement the pre-modular page had. Keep it free of
   imports, exports and outside references: the node suites lift
   this code out of the built page and eval it standalone. */
/* Match tier of a candidate name against the query: 2 = prefix,
   1 = substring, 0 = no match (case-insensitive). An empty query
   tiers everything 1 (neutral -- the empty state is ordered by
   recency and source order instead). */
function spotTier(q,name){
 const s=(q||'').trim().toLowerCase(); if(!s) return 1;
 const n=(name||'').toLowerCase();
 if(n.startsWith(s)) return 2;
 if(n.indexOf(s)>=0) return 1;
 return 0;
}
/* Rank {key,name} items for Spotlight: with a query, drop
   non-matches and order by tier (prefix before substring), then
   by recency weight (earlier in recentKeys = heavier), then by
   original order. Without a query, order by recency weight then
   original order. Input is not mutated. */
function spotRankList(items,q,recentKeys){
 const rec=recentKeys||[];
 const wgt=k=>{ const i=rec.indexOf(k); return i<0?0:rec.length-i; };
 const has=!!(q||'').trim();
 const dec=(items||[]).map((it,i)=>({it,i,t:spotTier(q,it.name),w:wgt(it.key)}));
 const keep=has?dec.filter(d=>d.t>0):dec;
 keep.sort((a,b)=>(b.t-a.t)||(b.w-a.w)||(a.i-b.i));
 return keep.map(d=>d.it);
}
