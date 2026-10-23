// Map page logic (feature 010, research R8): turns /ui/map.json into Cytoscape elements, decides
// what "Online only" shows and builds the details panel. No DOM or Cytoscape access here, so it
// is tested with node --test (internal/web/jstest).

const byId = (topo) => new Map([...topo.groups, ...topo.nodes].map((x) => [x.id, x]));

// tooltip is the hover text of a device: name · IP · MAC · last seen … ago.
export function tooltip(n) {
  const parts = [n.label, n.ip, n.mac].filter((s) => s);
  return parts.join(' · ') + ' · last seen ' + n.last_seen_ago + ' ago';
}

// toElements returns Cytoscape element definitions. A gateway of its own group is shown as a
// node class rather than an edge into its own box.
export function toElements(topo) {
  const els = [];
  for (const g of topo.groups) {
    els.push({ group: 'nodes', data: { id: g.id, label: g.label }, classes: 'group' + (g.stale ? ' stale' : '') });
  }
  for (const n of topo.nodes) {
    if (n.kind === 'port') {
      els.push({ group: 'nodes', data: { id: n.id, parent: n.parent, label: n.label }, classes: 'port' });
      continue;
    }
    const cls = ['device', 'state-' + n.state];
    if (n.state === 'offline') cls.push('dim');
    if (n.gateway_of && n.gateway_of.length) cls.push('gateway');
    els.push({ group: 'nodes', data: { id: n.id, parent: n.parent, label: n.label, tip: tooltip(n) }, classes: cls.join(' ') });
  }
  const parent = new Map(topo.nodes.map((n) => [n.id, n.parent]));
  for (const e of topo.edges) {
    if (e.kind === 'gateway' && parent.get(e.source) === e.target) continue;
    const data = { id: e.id, source: e.source, target: e.target };
    if (e.label) data.label = e.label;
    if (e.kind === 'manual') data.arrow = true;
    els.push({ group: 'edges', data, classes: 'kind-' + e.kind + (e.current === false ? ' last-known' : '') });
  }
  return els;
}

// visibleIds returns the ids (groups, nodes, edges of topo) to show. With onlineOnly, offline
// devices are hidden, with every edge touching a hidden element, ports left without a visible
// device, and groups left without a visible node. topo is not modified.
export function visibleIds(topo, onlineOnly) {
  const hidden = new Set();
  if (onlineOnly) {
    for (const n of topo.nodes) if (n.kind === 'device' && n.state === 'offline') hidden.add(n.id);
    for (const p of topo.nodes) {
      if (p.kind !== 'port') continue;
      const shown = topo.edges.some((e) => e.kind === 'port' && e.source === p.id && !hidden.has(e.target));
      if (!shown) hidden.add(p.id);
    }
    for (const g of topo.groups) {
      if (!topo.nodes.some((n) => n.parent === g.id && !hidden.has(n.id))) hidden.add(g.id);
    }
  }
  const out = new Set();
  for (const x of [...topo.groups, ...topo.nodes]) if (!hidden.has(x.id)) out.add(x.id);
  for (const e of topo.edges) if (!hidden.has(e.source) && !hidden.has(e.target)) out.add(e.id);
  return out;
}

// details returns the panel content for a device: title, state, status text (as on the
// Devices list), link and label/value rows (empty values omitted); null for anything else.
// Values are plain text: the page must render them with textContent.
export function details(topo, id) {
  const all = byId(topo);
  const n = all.get(id);
  if (!n || n.kind !== 'device') return null;
  const label = (x) => (all.get(x) ? all.get(x).label : x);
  let through = '';
  let via = '';
  for (const e of topo.edges) {
    if (e.kind === 'port' && e.target === id) {
      const router = topo.edges.find((r) => r.kind === 'port' && r.target === e.source);
      through = (e.label || label(e.source)) + (router ? ' on ' + label(router.source) : '') + (e.current === false ? ' (last known)' : '');
    }
    if (e.kind === 'manual' && e.source === id) via = label(e.target);
  }
  const rows = [
    ['Type', n.type],
    ['Addresses', (n.ips || []).join(', ')],
    ['MAC', n.mac],
    ['Vendor', n.vendor],
    ['Last seen', n.last_seen_ago ? n.last_seen_ago + ' ago' : ''],
    ['Gateway of', (n.gateway_of || []).map(label).join(', ')],
    ['Connected through', through],
    ['Connected via', via],
  ].filter(([, v]) => v).map(([l, v]) => ({ label: l, value: v }));
  return {
    title: n.label,
    state: n.state,
    statusText: n.state === 'offline' ? 'offline' : 'online',
    href: '/devices/' + n.device_id,
    rows,
  };
}
