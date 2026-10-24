// Map page (feature 010): draws /ui/map.json with the vendored Cytoscape. All decisions are in
// map-model.js (tested); this file only wires them to the page.
import { toElements, visibleIds, details, layout } from './map-model.js';

const box = document.getElementById('map');
const panel = document.getElementById('map-details');
const tip = document.getElementById('map-tip');

function color(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function styles() {
  const fg = color('--fg'), muted = color('--muted'), line = color('--line'), card = color('--card');
  const accent = color('--accent'), ok = color('--ok'), manual = color('--new');
  return [
    { selector: 'node', style: { label: 'data(label)', 'font-size': 11, color: fg, 'text-valign': 'bottom',
      'text-margin-y': 4, 'text-wrap': 'ellipsis', 'text-max-width': '120px', width: 16, height: 16,
      'background-color': muted } },
    { selector: 'node.group', style: { shape: 'round-rectangle', 'background-color': card, 'background-opacity': 1,
      'border-width': 1, 'border-color': line, 'text-valign': 'top', 'text-margin-y': -4, 'font-size': 12,
      'font-weight': 'bold', padding: 24, 'text-max-width': '400px' } },
    { selector: 'node.group.stale', style: { 'border-style': 'dashed' } },
    { selector: 'node.state-online', style: { 'background-color': ok } },
    { selector: 'node.state-stale', style: { 'background-color': fg } },
    { selector: 'node.state-offline', style: { 'background-color': muted } },
    { selector: 'node.dim', style: { opacity: 0.45 } },
    { selector: 'node.manual', style: { shape: 'round-rectangle', width: 20, 'background-color': card, 'border-width': 2,
      'border-style': 'dashed', 'border-color': fg } },
    { selector: 'node.gateway', style: { shape: 'diamond', width: 22, height: 22, 'border-width': 3, 'border-color': accent } },
    { selector: 'node.port', style: { shape: 'round-rectangle', width: 10, height: 10, 'background-color': line,
      'font-size': 10, color: muted } },
    { selector: 'edge', style: { width: 1, 'curve-style': 'bezier', 'line-color': line } },
    { selector: 'edge.kind-gateway', style: { width: 3, 'line-color': accent } },
    { selector: 'edge.kind-bridge', style: { width: 2, 'line-style': 'dashed', 'line-color': accent } },
    { selector: 'edge.kind-port', style: { 'line-color': muted } },
    { selector: 'edge.last-known', style: { 'line-style': 'dotted', opacity: 0.6 } },
    { selector: 'edge.kind-manual', style: { width: 2, 'line-color': manual, 'target-arrow-shape': 'triangle',
      'target-arrow-color': manual } },
    { selector: 'node.device:selected', style: { 'border-width': 3, 'border-color': accent } },
  ];
}

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

async function main() {
  if (!box || typeof cytoscape !== 'function') return;
  const res = await fetch('/ui/map.json', { cache: 'no-store' });
  if (!res.ok) {
    box.textContent = 'The map could not be loaded (' + res.status + ').';
    return;
  }
  const topo = await res.json();
  const cy = cytoscape({
    container: box,
    elements: toElements(topo),
    style: styles(),
    layout: { name: 'preset', positions: layout(topo), fit: true, padding: 30 },
    minZoom: 0.2,
    maxZoom: 3,
  });

  const apply = (onlineOnly) => {
    const show = visibleIds(topo, onlineOnly);
    cy.batch(() => cy.elements().forEach((el) => el.style('display', show.has(el.id()) ? 'element' : 'none')));
  };
  for (const r of document.querySelectorAll('input[name="map-scope"]')) {
    r.addEventListener('change', () => apply(r.value === 'online' && r.checked));
  }

  cy.on('tap', 'node.device', (ev) => showDetails(topo, ev.target.id()));
  cy.on('tap', (ev) => {
    if (ev.target === cy) panel.hidden = true;
  });
  cy.on('mouseover', 'node.device', (ev) => {
    const p = ev.renderedPosition;
    const r = box.getBoundingClientRect();
    tip.textContent = ev.target.data('tip');
    tip.style.left = (r.left + window.scrollX + p.x + 12) + 'px';
    tip.style.top = (r.top + window.scrollY + p.y + 12) + 'px';
    tip.hidden = false;
  });
  cy.on('mouseout', 'node.device', () => { tip.hidden = true; });
}

main();
