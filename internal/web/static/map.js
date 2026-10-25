// Map page (feature 010; tree layout: feature 014): lays /ui/map.json out with the vendored ELK and
// draws it as SVG. All decisions are in map-model.js (tested); this file only wires them to the page.
import { visibleIds, details, tooltip, elkGraph, place, nodeClass } from './map-model.js';

const box = document.getElementById('map');
const panel = document.getElementById('map-details');
const tip = document.getElementById('map-tip');
const fitButton = document.getElementById('map-fit');
const NS = 'http://www.w3.org/2000/svg';

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const cut = (s, n) => (s && s.length > n ? s.slice(0, n - 1) + '…' : s || '');

function showDetails(topo, id) {
  const d = details(topo, id);
  if (!d) {
    panel.hidden = true;
    return;
  }
  panel.replaceChildren();
  const h = document.createElement('h2');
  h.textContent = d.title + ' ';
  const badge = document.createElement('span');
  badge.className = 'badge s-' + d.state;
  badge.textContent = d.statusText;
  h.append(badge);
  const dl = document.createElement('dl');
  for (const r of d.rows) {
    const dt = document.createElement('dt');
    dt.textContent = r.label;
    const dd = document.createElement('dd');
    dd.textContent = r.value;
    dl.append(dt, dd);
  }
  const p = document.createElement('p');
  const a = document.createElement('a');
  a.href = d.href;
  a.textContent = 'Open device page';
  p.append(a);
  panel.append(h, dl, p);
  panel.hidden = false;
}

// svgFor draws the placed map. Every value from the data is escaped (names are plain text).
function svgFor(topo, show, p) {
  const nodes = new Map(topo.nodes.map((n) => [n.id, n]));
  const groups = new Map(topo.groups.map((g) => [g.id, g]));
  let s = '';
  for (const g of p.groups) {
    const info = groups.get(g.id) || {};
    s += `<g class="group${info.stale ? ' stale' : ''}"><rect x="${g.x}" y="${g.y}" width="${g.w}" height="${g.h}" rx="10"/>`
      + `<text x="${g.x + 14}" y="${g.y + 23}">${esc(info.label || g.id)}</text></g>`;
  }
  for (const b of p.blocks) s += `<rect class="block" x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" rx="8"/>`;
  for (const e of p.paths) {
    const cls = 'edge ' + e.kind + (e.current ? '' : ' last');
    s += `<path class="${cls}" d="M${e.points.map((pt) => pt.join(',')).join(' L')}"/>`;
  }
  // Gateways of another subnet: a dashed line to that subnet's box.
  const groupRect = new Map(p.groups.map((g) => [g.id, g]));
  for (const e of topo.edges) {
    if (e.kind !== 'gateway' || !show.has(e.id)) continue;
    const a = p.nodes.get(e.source), g = groupRect.get(e.target);
    const src = nodes.get(e.source);
    if (!a || !g || !src || src.parent === e.target) continue;
    const cx = a.x + a.w / 2, cy = a.y + a.h / 2;
    const tx = Math.min(Math.max(cx, g.x), g.x + g.w), ty = Math.min(Math.max(cy, g.y), g.y + g.h);
    const sx = tx < a.x ? a.x : tx > a.x + a.w ? a.x + a.w : cx;
    const sy = sx === cx ? (ty < cy ? a.y : a.y + a.h) : cy;
    s += `<path class="edge far" d="M${sx},${sy} L${tx},${ty}"/>`;
  }
  for (const [id, r] of p.nodes) {
    const n = nodes.get(id);
    if (!n) continue;
    if (n.kind === 'port') {
      s += `<g class="port"><rect x="${r.x}" y="${r.y}" width="${r.w}" height="${r.h}" rx="10"/>`
        + `<text x="${r.x + r.w / 2}" y="${r.y + 14}" text-anchor="middle">${esc(cut(n.label, 20))}</text></g>`;
      continue;
    }
    const sub = n.state === 'manual' ? 'added by hand' : [n.ip, n.type].filter(Boolean).join(' · ');
    s += `<g class="${nodeClass(n)}" data-id="${esc(id)}" tabindex="0" role="button" aria-label="${esc(n.label)}">`
      + `<rect x="${r.x}" y="${r.y}" width="${r.w}" height="${r.h}" rx="6"/>`
      + `<circle class="dot" cx="${r.x + 11}" cy="${r.y + 14}" r="4"/>`
      + `<text class="name" x="${r.x + 21}" y="${r.y + 18}">${esc(cut(n.label, 19))}</text>`
      + `<text class="sub" x="${r.x + 11}" y="${r.y + 33}">${esc(cut(sub, 26))}</text></g>`;
  }
  return s;
}

async function main() {
  if (!box || typeof ELK !== 'function') return;
  const res = await fetch('/ui/map.json', { cache: 'no-store' });
  if (!res.ok) {
    box.textContent = 'The map could not be loaded (' + res.status + ').';
    return;
  }
  const topo = await res.json();
  const elk = new ELK();
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('class', 'map-svg');
  const vp = document.createElementNS(NS, 'g');
  svg.append(vp);
  box.replaceChildren(svg);

  let view = { x: 0, y: 0, k: 1 };
  let size = { w: 0, h: 0 };
  const apply = () => vp.setAttribute('transform', `translate(${view.x},${view.y}) scale(${view.k})`);
  const fit = () => {
    const r = svg.getBoundingClientRect();
    if (!size.w || !r.width) return;
    view.k = Math.min(r.width / (size.w + 40), r.height / (size.h + 40), 1.25);
    view.x = (r.width - size.w * view.k) / 2;
    view.y = Math.max(10, (r.height - size.h * view.k) / 2);
    apply();
  };

  const draw = async (onlineOnly) => {
    const show = visibleIds(topo, onlineOnly);
    const r = svg.getBoundingClientRect();
    const aspect = r.width && r.height ? r.width / r.height : 1.6;
    const { graph, blocks, edges, cross } = elkGraph(topo, show, aspect);
    const p = place(await elk.layout(graph), blocks, edges, aspect);
    // A "connected via" across subnets is drawn straight, parent bottom to child top.
    for (const e of cross) {
      const a = p.nodes.get(e.source), b = p.nodes.get(e.target);
      if (a && b) p.paths.push({ id: e.id, kind: e.kind, current: e.current, points: [[a.x + a.w / 2, a.y + a.h], [b.x + b.w / 2, b.y]] });
    }
    vp.innerHTML = svgFor(topo, show, p);
    size = { w: p.width, h: p.height };
    fit();
  };
  for (const r of document.querySelectorAll('input[name="map-scope"]')) {
    r.addEventListener('change', () => draw(r.value === 'online' && r.checked));
  }
  if (fitButton) fitButton.addEventListener('click', fit);

  // Click: details (devices); background: close. Hover: tooltip.
  const deviceAt = (ev) => ev.target.closest && ev.target.closest('g.device');
  let moved = false;
  svg.addEventListener('click', (ev) => {
    if (moved) return;
    const g = deviceAt(ev);
    if (g) showDetails(topo, g.dataset.id);
    else panel.hidden = true;
  });
  svg.addEventListener('keydown', (ev) => {
    const g = deviceAt(ev);
    if (g && (ev.key === 'Enter' || ev.key === ' ')) {
      ev.preventDefault();
      showDetails(topo, g.dataset.id);
    }
  });
  const byId = new Map(topo.nodes.map((n) => [n.id, n]));
  svg.addEventListener('mousemove', (ev) => {
    const g = deviceAt(ev);
    if (!g || drag) {
      tip.hidden = true;
      return;
    }
    tip.textContent = tooltip(byId.get(g.dataset.id));
    tip.style.left = (ev.pageX + 12) + 'px';
    tip.style.top = (ev.pageY + 12) + 'px';
    tip.hidden = false;
  });
  svg.addEventListener('mouseleave', () => { tip.hidden = true; });

  // Pan (drag) and zoom (wheel, around the pointer).
  let drag = null;
  svg.addEventListener('mousedown', (ev) => {
    drag = { x: ev.clientX, y: ev.clientY, vx: view.x, vy: view.y };
    moved = false;
  });
  window.addEventListener('mousemove', (ev) => {
    if (!drag) return;
    const dx = ev.clientX - drag.x, dy = ev.clientY - drag.y;
    if (Math.abs(dx) + Math.abs(dy) > 3) moved = true;
    view.x = drag.vx + dx;
    view.y = drag.vy + dy;
    apply();
  });
  window.addEventListener('mouseup', () => { drag = null; });
  svg.addEventListener('wheel', (ev) => {
    ev.preventDefault();
    const f = ev.deltaY < 0 ? 1.12 : 1 / 1.12;
    const k = Math.min(3, Math.max(0.15, view.k * f));
    const r = svg.getBoundingClientRect();
    const px = ev.clientX - r.left, py = ev.clientY - r.top;
    view.x = px - (px - view.x) * (k / view.k);
    view.y = py - (py - view.y) * (k / view.k);
    view.k = k;
    apply();
  }, { passive: false });
  // The details panel and the window change the map's width: fit again.
  if (typeof ResizeObserver === 'function') new ResizeObserver(fit).observe(box);
  else window.addEventListener('resize', fit);

  await draw(false);
}

main().catch((err) => {
  if (box) box.textContent = 'The map could not be drawn: ' + err;
});
