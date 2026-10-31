/* Pure functions (node-testable): this file is NOT bundled. The
   build transforms it on its own and inlines the result between
   the "dockmag pure" marker comments in the page, where these
   declarations become page globals shared by every bundle -- the
   same arrangement the pre-modular page had. Keep it free of
   imports, exports and outside references: the node suites lift
   this code out of the built page and eval it standalone. */
/* Raised-cosine magnification, the macOS model: the icon under the
   pointer reaches the peak ratio, neighbours fall off on a cosine
   curve (flat at the peak and at the rim, so the motion has no
   kinks), and anything at or beyond the radius stays at 1. dist
   and radius in px; the sign of dist is ignored (symmetric). */
function dockMagScale(dist,radius,peak){
 const d=Math.abs(dist);
 if(!(radius>0)||d>=radius) return 1;
 return 1+(peak-1)*0.5*(1+Math.cos(Math.PI*d/radius));
}
function dockMagSizes(centers,pointer,base,radius,peak){
 return centers.map(c=>base*dockMagScale(c-pointer,radius,peak));
}
