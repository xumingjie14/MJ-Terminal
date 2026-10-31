/* ----- unified chrome glyphs (v0.56 test2) -----
   ONE self-drawn monochrome line-glyph set for ALL UI chrome
   (menubar, context menus, Control Center, Settings sidebar,
   Spotlight actions, notifications): SF-Symbols grammar --
   24-grid, uniform 1.7 stroke, round caps/joins, currentColor.
   This replaces the emoji and mixed-vendor glyphs the user
   field-rejected on test1 ("整套设计风格都要统一高度一致").
   Emoji remains only inside user content, never as chrome. */
export const GLYPHS={
folder:'<path d="M3.5 7a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v8.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/>',
folderplus:'<path d="M3.5 7a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v8.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/><path d="M12 10.8v5.4M9.3 13.5h5.4"/>',
folderup:'<path d="M3.5 7a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v8.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/><path d="M12 16.8v-5m0 0-2.3 2.3M12 11.8l2.3 2.3"/>',
import:'<path d="M12 3.8v9.7m0 0 3.8-3.8M12 13.5 8.2 9.7"/><path d="M3.8 15.8v2.7A1.7 1.7 0 0 0 5.5 20.2h13a1.7 1.7 0 0 0 1.7-1.7v-2.7"/>',
export:'<path d="M12 13.5V3.8m0 0L8.2 7.6M12 3.8l3.8 3.8"/><path d="M3.8 13.5v5a1.7 1.7 0 0 0 1.7 1.7h13a1.7 1.7 0 0 0 1.7-1.7v-5"/>',
size:'<path d="M4 9.2V5.8A1.8 1.8 0 0 1 5.8 4h3.4M14.8 4h3.4A1.8 1.8 0 0 1 20 5.8v3.4M20 14.8v3.4a1.8 1.8 0 0 1-1.8 1.8h-3.4M9.2 20H5.8A1.8 1.8 0 0 1 4 18.2v-3.4"/>',
sort:'<path d="M11 6h8.5M11 12h6M11 18h3.5"/><path d="M5.3 4.8v14.4m0 0L2.9 16.6m2.4 2.6 2.4-2.6"/>',
refresh:'<path d="M19.6 12a7.6 7.6 0 1 1-2.2-5.4"/><path d="M19.9 3.6v3.8h-3.8"/>',
palette:'<circle cx="12" cy="12" r="8.6"/><circle cx="9" cy="9.3" r="1.2" fill="currentColor" stroke="none"/><circle cx="14.9" cy="8.7" r="1.2" fill="currentColor" stroke="none"/><circle cx="15.6" cy="14.3" r="1.2" fill="currentColor" stroke="none"/><path d="M12 20.6c-1.2 0-1.8-.9-1.8-1.8 0-1.4 1.6-1.7 1.6-3.1"/>',
open:'<path d="M9.5 4.8H6A1.8 1.8 0 0 0 4.2 6.6v11A1.8 1.8 0 0 0 6 19.4h11a1.8 1.8 0 0 0 1.8-1.8V14"/><path d="M13.8 4.5h5.5v5.5M19.2 4.6 11 12.8"/>',
rename:'<path d="M4.2 19.8 5.4 15.6 16.9 4.1a2.15 2.15 0 0 1 3 3L8.4 18.6z"/><path d="M14.8 6.1l3 3"/>',
trash:'<path d="M4.5 6.3h15M9.8 6V4.9A1 1 0 0 1 10.8 3.9h2.4a1 1 0 0 1 1 1.1V6M6.4 6.3l.9 11.9A1.8 1.8 0 0 0 9.1 20h5.8a1.8 1.8 0 0 0 1.8-1.8l.9-11.9"/><path d="M10.2 10.3v5.9M13.8 10.3v5.9"/>',
window:'<rect x="4" y="5" width="16" height="14.5" rx="2"/><path d="M4 9.3h16"/><circle cx="6.9" cy="7.1" r=".95" fill="currentColor" stroke="none"/><circle cx="10.2" cy="7.1" r=".95" fill="currentColor" stroke="none"/>',
eyeoff:'<path d="M4.6 5.9A13.6 13.6 0 0 0 3 12c1.7 3.4 5 5.5 9 5.5 1.5 0 2.9-.3 4.1-.9M10 4.9c.6-.1 1.3-.2 2-.2 4 0 7.3 2.1 9 5.5a13.4 13.4 0 0 1-2.4 3.1"/><path d="M4 4.2 20 19.8"/>',
minimize:'<path d="M5.5 12h13"/>',
closex:'<path d="M6 6l12 12M18 6 6 18"/>',
switch:'<path d="M4 8.3 12 4l8 4.3-8 4.3z"/><path d="m4.6 13.2 7.4 4 7.4-4"/>',
check:'<path d="m5 12.8 4.4 4.4L19 7.6"/>',
checkcircle:'<circle cx="12" cy="12" r="8.6"/><path d="m8.4 12.4 2.6 2.6 4.8-5.2"/>',
xcircle:'<circle cx="12" cy="12" r="8.6"/><path d="m9.2 9.2 5.6 5.6M14.8 9.2l-5.6 5.6"/>',
warn:'<path d="M12 4.3 21.6 20H2.4z"/><path d="M12 9.8v4.4"/><circle cx="12" cy="16.9" r="1.15" fill="currentColor" stroke="none"/>',
wifi:'<path d="M4.3 10a11.2 11.2 0 0 1 15.4 0M7 13.3a7.2 7.2 0 0 1 10 0M9.8 16.4a3.6 3.6 0 0 1 4.4 0"/><circle cx="12" cy="19.2" r="1.25" fill="currentColor" stroke="none"/>',
battery:'<rect x="2.8" y="8.3" width="16.7" height="7.8" rx="2.2"/><path d="M21.9 10.9v2.6"/><rect x="5.1" y="10.6" width="6" height="3.2" rx="1" fill="currentColor" stroke="none"/>',
stage:'<rect x="4" y="4" width="7" height="7" rx="1.9"/><rect x="13" y="4" width="7" height="7" rx="1.9"/><rect x="4" y="13" width="7" height="7" rx="1.9"/><rect x="13" y="13" width="7" height="7" rx="1.9"/>',
rocket:'<path d="M12 3c2.9 1.9 4.5 5.2 4.5 8.6L12 16l-4.5-4.4C7.5 8.2 9.1 4.9 12 3z"/><circle cx="12" cy="9.3" r="1.9"/><path d="M7.6 13.4 4.8 17.2l3-.7M16.4 13.4l2.8 3.8-3-.7M10.4 18.9c.5 1.1 2.7 1.1 3.2 0"/>',
half:'<circle cx="12" cy="12" r="8.6"/><path d="M12 3.4a8.6 8.6 0 0 1 0 17.2z" fill="currentColor" stroke="none"/>',
dock:'<rect x="5.8" y="4.5" width="5" height="5" rx="1.4"/><rect x="13.2" y="4.5" width="5" height="5" rx="1.4"/><rect x="3.5" y="14" width="17" height="5.8" rx="2.4"/>',
link:'<path d="M9.9 13.9a4.1 4.1 0 0 0 5.9.4l2.9-2.9a4.24 4.24 0 1 0-6-6l-1.6 1.6"/><path d="M14.1 10.1a4.1 4.1 0 0 0-5.9-.4l-2.9 2.9a4.24 4.24 0 1 0 6 6l1.6-1.6"/>',
image:'<rect x="3.5" y="5" width="17" height="14.5" rx="2"/><circle cx="9" cy="10" r="1.5"/><path d="m5 17.5 5-4.7 3.4 3 2.4-2.3 3.7 3.6"/>',
bell:'<path d="M6.2 16.3v-4.8a5.8 5.8 0 0 1 11.6 0v4.8l1.3 2.8a.65.65 0 0 1-.6.9H5.5a.65.65 0 0 1-.6-.9z"/><path d="M10.2 20.4a2 2 0 0 0 3.6 0"/>',
globe:'<circle cx="12" cy="12" r="8.6"/><path d="M3.4 12h17.2M12 3.4c2.6 2.3 3.9 5.2 3.9 8.6s-1.3 6.3-3.9 8.6c-2.6-2.3-3.9-5.2-3.9-8.6s1.3-6.3 3.9-8.6z"/>',
shapes:'<circle cx="8" cy="8" r="3.6"/><rect x="14" y="4.4" width="6.4" height="6.4" rx="1.6"/><path d="M8 13.8 11.8 20H4.2z"/><circle cx="16.9" cy="16.9" r="3.1"/>',
box:'<path d="M12 3.4 20.2 7.6v8.8L12 20.6 3.8 16.4V7.6z"/><path d="m3.9 7.6 8.1 4.1 8.1-4.1M12 11.7v8.8"/>',
stack:'<path d="m12 3.6 9 4.6-9 4.6-9-4.6z"/><path d="m4 12.4 8 4.1 8-4.1M4 16.4l8 4.1 8-4.1"/>',
lock:'<rect x="5.3" y="10.4" width="13.4" height="9.7" rx="2.2"/><path d="M8.4 10.4V7.9a3.6 3.6 0 0 1 7.2 0v2.5"/><circle cx="12" cy="15.2" r="1.35" fill="currentColor" stroke="none"/>',
gear:'<circle cx="12" cy="12" r="3.1"/><g><rect x="11" y="2.8" width="2" height="4.4" rx="1"/><rect x="11" y="2.8" width="2" height="4.4" rx="1" transform="rotate(45 12 12)"/><rect x="11" y="2.8" width="2" height="4.4" rx="1" transform="rotate(90 12 12)"/><rect x="11" y="2.8" width="2" height="4.4" rx="1" transform="rotate(135 12 12)"/><rect x="11" y="2.8" width="2" height="4.4" rx="1" transform="rotate(180 12 12)"/><rect x="11" y="2.8" width="2" height="4.4" rx="1" transform="rotate(225 12 12)"/><rect x="11" y="2.8" width="2" height="4.4" rx="1" transform="rotate(270 12 12)"/><rect x="11" y="2.8" width="2" height="4.4" rx="1" transform="rotate(315 12 12)"/></g><circle cx="12" cy="12" r="6.9"/>',
info:'<circle cx="12" cy="12" r="8.6"/><path d="M12 11.2V16"/><circle cx="12" cy="7.9" r="1.25" fill="currentColor" stroke="none"/>',
up:'<circle cx="12" cy="12" r="8.6"/><path d="M12 16.6v-9m0 0-4 4m4-4 4 4"/>',
power:'<path d="M12 3.4v7.8"/><path d="M6.2 6.3a8.2 8.2 0 1 0 11.6 0"/>',
list:'<path d="M8.6 6.4h11M8.6 12h11M8.6 17.6h11"/><circle cx="4.9" cy="6.4" r="1.15" fill="currentColor" stroke="none"/><circle cx="4.9" cy="12" r="1.15" fill="currentColor" stroke="none"/><circle cx="4.9" cy="17.6" r="1.15" fill="currentColor" stroke="none"/>',
grid:'<rect x="4" y="4" width="16" height="16" rx="2"/><path d="M4 9.3h16M4 14.6h16M9.3 4v16M14.6 4v16"/>',
file:'<path d="M6.6 3.6h5.9L18 9.1v10.2a1.3 1.3 0 0 1-1.3 1.3H6.6a1.3 1.3 0 0 1-1.3-1.3V4.9a1.3 1.3 0 0 1 1.3-1.3z"/><path d="M12.3 3.8v5.4H18"/>',
music:'<circle cx="7" cy="17.8" r="2.7"/><circle cx="17.4" cy="15.8" r="2.7"/><path d="M9.7 17.8V6.4L20.1 4.6v11.2"/>',
app:'<rect x="4" y="4" width="16" height="16" rx="4.6"/><rect x="9.2" y="9.2" width="5.6" height="5.6" rx="1.7"/>',
};
export function glyph(n,s){
 return `<svg class="gly" width="${s||16}" height="${s||16}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${GLYPHS[n]||''}</svg>`;
}
