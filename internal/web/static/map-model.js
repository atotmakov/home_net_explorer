// Map page logic (feature 010, research R8; tree layout: feature 014): turns /ui/map.json into a
// top-down tree for ELK, places the result, decides what "Online only" shows and builds the
// details panel. No DOM access here, so it is tested with node --test (internal/web/jstest).

const byId = (topo) => new Map([...topo.groups, ...topo.nodes].map((x) => [x.id, x]));

const MANUAL = 'manual (added by hand)';

// tooltip is the hover text of a device: name · IP · MAC · last seen … ago, or "manual (added by
// hand)" for a device the owner added (feature 011).
export function tooltip(n) {
  const parts = [n.label, n.ip, n.mac].filter((s) => s);
  if (n.state === 'manual') return parts.concat(MANUAL).join(' · ');
  return parts.join(' · ') + ' · last seen ' + n.last_seen_ago + ' ago';
}

// visibleIds returns the ids (groups, nodes, edges of topo) to show. With onlineOnly, offline
// devices are hidden, with every edge touching a hidden element, ports left without a visible
// device, and groups left without a visible node. A manual device stays when an online or stale
// device connects via it, directly or through other manual devices (feature 011, FR-006). topo is
// not modified.
export function visibleIds(topo, onlineOnly) {
  const hidden = new Set();
  if (onlineOnly) {
    for (const n of topo.nodes) if (n.kind === 'device' && n.state === 'offline') hidden.add(n.id);
    const state = new Map(topo.nodes.map((n) => [n.id, n.state]));
    const kept = new Set();
    for (let grew = true; grew;) {
      grew = false;
      for (const e of topo.edges) {
        if (e.kind !== 'manual' || state.get(e.target) !== 'manual' || kept.has(e.target)) continue;
        const from = state.get(e.source);
        if (from === 'online' || from === 'stale' || (from === 'manual' && kept.has(e.source))) {
          kept.add(e.target);
          grew = true;
        }
      }
    }
    for (const n of topo.nodes) if (n.state === 'manual' && !kept.has(n.id)) hidden.add(n.id);
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
    statusText: n.state === 'manual' ? MANUAL : n.state === 'offline' ? 'offline' : 'online',
    href: '/devices/' + n.device_id,
    rows,
  };
}

// SIZE is the drawn size of a device box (w × h), a port / Wi-Fi label (pw × ph) and the gap
// between devices in a block (feature 014).
export const SIZE = { w: 168, h: 40, pw: 120, ph: 20, gap: 8 };

// nodeClass is the CSS class list of a device box: device, its state, dim when offline, gateway.
export function nodeClass(n) {
  const cls = ['device', n.state];
  if (n.state === 'offline') cls.push('dim');
  if ((n.gateway_of || []).length) cls.push('gateway');
  return cls.join(' ');
}

// tree returns the parent → child edges among the shown elements (plan R2): router → port and
// port → device as they are; the owner's "connected via" (child → parent) reversed; and, for a
// device of a subnet with a gateway and no parent, a dotted edge from the gateway ("same subnet,
// wiring unknown") unless the device is an ancestor of the gateway (a loop).
// Each edge: {id, source, target, kind: port|manual|unknown, current}.
export function tree(topo, show) {
  const out = [];
  const parent = new Map();
  for (const e of topo.edges) {
    if (!show.has(e.id) || !show.has(e.source) || !show.has(e.target)) continue;
    if (e.kind === 'port') {
      out.push({ id: e.id, source: e.source, target: e.target, kind: 'port', current: e.current !== false });
      parent.set(e.target, e.source);
    } else if (e.kind === 'manual') {
      out.push({ id: e.id, source: e.target, target: e.source, kind: 'manual', current: true });
      parent.set(e.source, e.target);
    }
  }
  const ancestorOf = (a, b) => { // a is on the parent chain of b
    const seen = new Set();
    for (let cur = parent.get(b); cur && !seen.has(cur); cur = parent.get(cur)) {
      if (cur === a) return true;
      seen.add(cur);
    }
    return false;
  };
  for (const g of topo.groups) {
    if (!show.has(g.id)) continue;
    const gw = topo.nodes.find((n) => n.kind === 'device' && n.parent === g.id && show.has(n.id) && (n.gateway_of || []).includes(g.id));
    if (!gw) continue;
    for (const n of topo.nodes) {
      if (n.kind !== 'device' || n.parent !== g.id || n.id === gw.id || !show.has(n.id) || parent.has(n.id)) continue;
      if (ancestorOf(n.id, gw.id)) continue;
      out.push({ id: 'u-' + n.id, source: gw.id, target: n.id, kind: 'unknown', current: true });
      parent.set(n.id, gw.id);
    }
  }
  return out;
}

// elkGraph returns the ELK input for the shown part of topo: one compound node per subnet, laid out
// as its own top-down tree (layered) with its ports, devices and tree edges, the subnets packed
// as rectangles towards aspect (width / height of the screen); and blocks: three or more childless
// devices of one parent (in the parent's subnet) become one node of 2 columns (3 when more than 6)
// with one edge from the parent (plan R3). blocks maps a block id to {members, cols, group}. A tree
// edge between two subnets (rare: a "connected via" across subnets) is not laid out; it is
// returned in cross and drawn straight.
export function elkGraph(topo, show, aspect = 1.6) {
  const edges = tree(topo, show);
  const nodeById = new Map(topo.nodes.map((n) => [n.id, n]));
  const hasKids = new Set(edges.map((e) => e.source));
  const leaves = new Map();
  for (const e of edges) {
    const n = nodeById.get(e.target);
    if (!n || n.kind !== 'device' || hasKids.has(e.target)) continue;
    if (!leaves.has(e.source)) leaves.set(e.source, []);
    leaves.get(e.source).push(e.target);
  }
  const blocks = new Map();
  const inBlock = new Map();
  for (const [p, members] of leaves) {
    const g = nodeById.get(p) ? nodeById.get(p).parent : undefined;
    const same = members.filter((m) => nodeById.get(m).parent === g);
    if (same.length < 3) continue;
    const id = 'block-' + p;
    blocks.set(id, { members: same, cols: same.length > 6 ? 3 : 2, group: g });
    for (const m of same) inBlock.set(m, id);
  }
  const groupEdges = new Map();
  const cross = [];
  const linked = new Set();
  for (const e of edges) {
    const g = nodeById.get(e.source).parent;
    if (nodeById.get(e.target).parent !== g) {
      cross.push(e);
      continue;
    }
    if (!groupEdges.has(g)) groupEdges.set(g, []);
    const b = inBlock.get(e.target);
    if (!b) {
      groupEdges.get(g).push({ id: e.id, sources: [e.source], targets: [e.target] });
    } else if (!linked.has(b)) {
      linked.add(b);
      groupEdges.get(g).push({ id: 'eb-' + b, sources: [e.source], targets: [b] });
    }
  }
  const children = topo.groups.filter((g) => show.has(g.id)).map((g) => ({
    id: g.id,
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'DOWN',
      'elk.edgeRouting': 'ORTHOGONAL',
      'elk.padding': '[top=36,left=16,bottom=16,right=16]',
      'elk.layered.spacing.nodeNodeBetweenLayers': '36',
      'elk.spacing.nodeNode': '14',
      'elk.spacing.componentComponent': '24',
      'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
    },
    edges: groupEdges.get(g.id) || [],
    children: [
      ...topo.nodes.filter((n) => n.parent === g.id && show.has(n.id) && !inBlock.has(n.id)).map((n) => (n.kind === 'port'
        ? { id: n.id, width: SIZE.pw, height: SIZE.ph }
        : { id: n.id, width: SIZE.w, height: SIZE.h })),
      ...[...blocks].filter(([, b]) => b.group === g.id).map(([id, b]) => ({
        id,
        width: b.cols * (SIZE.w + SIZE.gap) + SIZE.gap,
        height: Math.ceil(b.members.length / b.cols) * (SIZE.h + SIZE.gap) + SIZE.gap,
      })),
    ],
  })).filter((g) => g.children.length);
  const graph = {
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'rectpacking',
      'elk.hierarchyHandling': 'SEPARATE_CHILDREN',
      'elk.aspectRatio': String(aspect),
      'elk.spacing.nodeNode': '24',
      'elk.padding': '[top=8,left=8,bottom=8,right=8]',
    },
    children,
    edges: [],
  };
  return { graph, blocks, edges, cross };
}

// shelf places boxes ({id, w, h}) in rows, tallest first, each row filled left to right up to a
// width that gives the whole about the aspect ratio (width / height) of the screen; never narrower
// than the widest box. Returns {positions: Map id → {x, y}, width, height}.
export function shelf(boxes, aspect = 1.6, gap = 24) {
  const area = boxes.reduce((a, b) => a + (b.w + gap) * (b.h + gap), 0);
  const limit = Math.max(Math.sqrt(area * aspect), ...boxes.map((b) => b.w), 0);
  const order = [...boxes].sort((a, b) => b.h - a.h);
  const positions = new Map();
  let x = 0, y = 0, rowH = 0, width = 0;
  for (const b of order) {
    if (x > 0 && x + b.w > limit) {
      y += rowH + gap;
      x = 0;
      rowH = 0;
    }
    positions.set(b.id, { x, y });
    x += b.w + gap;
    rowH = Math.max(rowH, b.h);
    width = Math.max(width, x - gap);
  }
  return { positions, width, height: order.length ? y + rowH : 0 };
}

// place turns an ELK result into absolute rectangles: groups (shelf-packed towards aspect),
// blocks and nodes (block members on the block's grid), and edge paths ({id, kind, current,
// points}); edges are tree's, for the kinds.
export function place(res, blocks, edges = [], aspect = 1.6) {
  const packed = shelf((res.children || []).map((g) => ({ id: g.id, w: g.width, h: g.height })), aspect);
  const out = { width: packed.width, height: packed.height, groups: [], blocks: [], nodes: new Map(), paths: [] };
  const byId = new Map(edges.map((e) => [e.id, e]));
  const kindOf = (id) => {
    if (byId.has(id)) return byId.get(id);
    if (id.startsWith('eb-') && blocks.has(id.slice(3))) {
      const members = blocks.get(id.slice(3)).members;
      return edges.find((x) => members.includes(x.target));
    }
    return undefined;
  };
  const walk = (n, x0, y0, depth) => {
    for (const e of n.edges || []) {
      const k = kindOf(e.id);
      for (const sec of e.sections || []) {
        const pts = [sec.startPoint, ...(sec.bendPoints || []), sec.endPoint].map((pt) => [x0 + pt.x, y0 + pt.y]);
        out.paths.push({ id: e.id, kind: k ? k.kind : 'port', current: k ? k.current : true, points: pts });
      }
    }
    for (const c of n.children || []) {
      const at = depth === 0 ? packed.positions.get(c.id) : { x: x0 + c.x, y: y0 + c.y };
      const r = { id: c.id, x: at.x, y: at.y, w: c.width, h: c.height };
      if (depth === 0) {
        out.groups.push(r);
      } else if (blocks.has(c.id)) {
        out.blocks.push(r);
        const b = blocks.get(c.id);
        b.members.forEach((m, i) => out.nodes.set(m, {
          x: r.x + SIZE.gap + (i % b.cols) * (SIZE.w + SIZE.gap),
          y: r.y + SIZE.gap + Math.floor(i / b.cols) * (SIZE.h + SIZE.gap),
          w: SIZE.w, h: SIZE.h,
        }));
      } else {
        out.nodes.set(c.id, { x: r.x, y: r.y, w: r.w, h: r.h });
      }
      walk(c, r.x, r.y, depth + 1);
    }
  };
  walk(res, 0, 0, 0);
  return out;
}
