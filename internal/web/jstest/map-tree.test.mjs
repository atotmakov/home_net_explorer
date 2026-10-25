// Feature 014: the Map as a top-down tree laid out by ELK (spec FR-001..FR-005, plan R2, R3).
// Pure model functions, plus real ELK layouts run in Node with the vendored elkjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { tree, elkGraph, place, nodeClass, visibleIds, SIZE } from '../static/map-model.js';

const require = createRequire(import.meta.url);
const ELK = require('../static/vendor/elk.bundled.js');

const dev = (id, parent, label, state, extra = {}) => ({
  id, kind: 'device', parent, device_id: Number(id.slice(4)), label, ip: '192.168.0.' + id.slice(4),
  ips: ['192.168.0.' + id.slice(4)], mac: '', vendor: '', type: '', state, last_seen_ago: '3 mins',
  gateway_of: [], ...extra,
});
const port = (id, parent, label) => ({ id, kind: 'port', parent, label });

// Router .1 (gateway) with LAN1 (pc, tv offline) and Wi-Fi under an AP; a switch added by hand
// via the router, with a NAS behind it; DUNE with no known parent; an old camera in a subnet with
// no gateway.
function net() {
  return {
    groups: [
      { id: 'subnet-2', label: '192.168.0.0/24', cidr: '192.168.0.0/24', stale: false },
      { id: 'subnet-3', label: '192.168.8.0/24', cidr: '192.168.8.0/24', stale: false },
    ],
    nodes: [
      dev('dev-1', 'subnet-2', 'HG8145V5', 'online', { type: 'router', gateway_of: ['subnet-2'] }),
      port('port-1-LAN1', 'subnet-2', 'LAN1'),
      dev('dev-4', 'subnet-2', 'pc', 'online'),
      dev('dev-5', 'subnet-2', 'tv', 'offline'),
      dev('dev-20', 'subnet-2', 'TL-SL1218P', 'manual', { ip: '', ips: [], last_seen_ago: '' }),
      dev('dev-21', 'subnet-2', 'nas', 'online'),
      dev('dev-9', 'subnet-2', 'EAP225', 'online', { type: 'access_point' }),
      port('port-9-Home (5 GHz)', 'subnet-2', 'Home (5 GHz)'),
      dev('dev-30', 'subnet-2', 'phone', 'online'),
      dev('dev-7', 'subnet-2', 'DUNE', 'stale', { ips: ['192.168.0.7', '192.168.8.7'] }),
      dev('dev-40', 'subnet-3', 'old cam', 'offline'),
    ],
    edges: [
      { id: 'gw-1-subnet-2', kind: 'gateway', source: 'dev-1', target: 'subnet-2', current: true },
      { id: 'rp-1-LAN1', kind: 'port', source: 'dev-1', target: 'port-1-LAN1', label: 'LAN1', current: true },
      { id: 'pd-4', kind: 'port', source: 'port-1-LAN1', target: 'dev-4', label: 'LAN1', current: true },
      { id: 'pd-5', kind: 'port', source: 'port-1-LAN1', target: 'dev-5', label: 'LAN1', current: false },
      { id: 'mu-20', kind: 'manual', source: 'dev-20', target: 'dev-1', current: true },
      { id: 'mu-21', kind: 'manual', source: 'dev-21', target: 'dev-20', current: true },
      { id: 'mu-9', kind: 'manual', source: 'dev-9', target: 'dev-20', current: true },
      { id: 'rp-9-Home (5 GHz)', kind: 'port', source: 'dev-9', target: 'port-9-Home (5 GHz)', label: 'Home (5 GHz)', current: true },
      { id: 'pd-30', kind: 'port', source: 'port-9-Home (5 GHz)', target: 'dev-30', label: 'Home (5 GHz)', current: true },
      { id: 'br-7-subnet-3', kind: 'bridge', source: 'dev-7', target: 'subnet-3', current: true },
    ],
  };
}

const all = (t) => visibleIds(t, false);
const asMap = (es) => Object.fromEntries(es.map((e) => [e.id, e]));

test('tree: ports down, manual links reversed, unknown wiring under the gateway', () => {
  const t = net();
  const es = asMap(tree(t, all(t)));
  assert.deepEqual(Object.keys(es).sort(), ['mu-20', 'mu-21', 'mu-9', 'pd-30', 'pd-4', 'pd-5',
    'rp-1-LAN1', 'rp-9-Home (5 GHz)', 'u-dev-7'].sort());
  assert.deepEqual([es['rp-1-LAN1'].source, es['rp-1-LAN1'].target, es['rp-1-LAN1'].kind], ['dev-1', 'port-1-LAN1', 'port']);
  assert.equal(es['pd-5'].current, false);
  // "Connected via" points child → parent; the tree goes parent → child.
  assert.deepEqual([es['mu-20'].source, es['mu-20'].target, es['mu-20'].kind], ['dev-1', 'dev-20', 'manual']);
  assert.deepEqual([es['mu-21'].source, es['mu-21'].target], ['dev-20', 'dev-21']);
  // DUNE has no parent: dotted under the gateway. The old cam's subnet has no gateway: nothing.
  assert.deepEqual([es['u-dev-7'].source, es['u-dev-7'].target, es['u-dev-7'].kind], ['dev-1', 'dev-7', 'unknown']);
  assert.ok(!Object.values(es).some((e) => e.target === 'dev-40'));
  assert.deepEqual(tree({ groups: [], nodes: [], edges: [] }, new Set()), []);
});

test('tree: no dotted edge that would close a loop; only visible elements', () => {
  const t = net();
  // The gateway itself connects via the pc: the pc must not hang under the gateway.
  t.edges = t.edges.filter((e) => e.id !== 'pd-4');
  t.edges.push({ id: 'mu-1', kind: 'manual', source: 'dev-1', target: 'dev-4', current: true });
  const es = tree(t, all(t));
  assert.ok(!es.some((e) => e.kind === 'unknown' && e.target === 'dev-4'));

  const u = net();
  const online = tree(u, visibleIds(u, true));
  assert.ok(!online.some((e) => e.target === 'dev-5'), 'offline tv hidden');
  assert.ok(online.some((e) => e.target === 'dev-4'));
});

test('nodeClass', () => {
  const n = asMap(net().nodes);
  assert.equal(nodeClass(n['dev-1']), 'device online gateway');
  assert.equal(nodeClass(n['dev-5']), 'device offline dim');
  assert.equal(nodeClass(n['dev-7']), 'device stale');
  assert.equal(nodeClass(n['dev-20']), 'device manual');
});

test('elkGraph: subnet boxes, sizes, leaf blocks', () => {
  const t = net();
  const { graph, blocks } = elkGraph(t, all(t));
  assert.equal(graph.layoutOptions['elk.algorithm'], 'layered');
  assert.equal(graph.layoutOptions['elk.direction'], 'DOWN');
  assert.deepEqual(graph.children.map((g) => g.id), ['subnet-2', 'subnet-3']);
  const kids = asMap(graph.children[0].children);
  assert.deepEqual([kids['dev-4'].width, kids['dev-4'].height], [SIZE.w, SIZE.h]);
  assert.deepEqual([kids['port-1-LAN1'].width, kids['port-1-LAN1'].height], [SIZE.pw, SIZE.ph]);
  assert.equal(blocks.size, 0, 'two leaves under LAN1: no block');

  // Three and seven childless devices under one parent become one block (2 and 3 columns).
  for (const [count, cols] of [[3, 2], [7, 3]]) {
    const u = net();
    for (let i = 0; i < count - 2; i++) {
      u.nodes.push(dev('dev-' + (50 + i), 'subnet-2', 'cam' + i, 'online'));
      u.edges.push({ id: 'pd-' + (50 + i), kind: 'port', source: 'port-1-LAN1', target: 'dev-' + (50 + i), current: true });
    }
    const g = elkGraph(u, all(u));
    const b = g.blocks.get('block-port-1-LAN1');
    assert.ok(b, `${count} leaves: a block`);
    assert.equal(b.members.length, count);
    assert.equal(b.cols, cols);
    const ids = g.graph.children[0].children.map((c) => c.id);
    assert.ok(ids.includes('block-port-1-LAN1') && !ids.includes('dev-4'), 'members are inside the block');
    const toBlock = g.graph.edges.filter((e) => e.targets[0] === 'block-port-1-LAN1');
    assert.equal(toBlock.length, 1);
    assert.equal(toBlock[0].sources[0], 'port-1-LAN1');
  }
});

async function laidOut(t, show = all(t)) {
  const { graph, blocks } = elkGraph(t, show);
  const res = await new ELK().layout(graph);
  return { p: place(res, blocks), edges: tree(t, show) };
}

const overlap = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

async function checkLayout(t) {
  const { p, edges } = await laidOut(t);
  const byNode = p.nodes;
  // Every child is below its parent (spec FR-001).
  for (const e of edges) {
    const a = byNode.get(e.source), b = byNode.get(e.target);
    assert.ok(a && b, e.id);
    assert.ok(b.y >= a.y + a.h, `${e.target} (y ${b.y}) is not below ${e.source} (bottom ${a.y + a.h})`);
  }
  // No two device or port boxes overlap; each is inside its subnet box.
  const rects = [...byNode.entries()];
  for (let i = 0; i < rects.length; i++) {
    for (let j = i + 1; j < rects.length; j++) {
      assert.ok(!overlap(rects[i][1], rects[j][1]), `${rects[i][0]} overlaps ${rects[j][0]}`);
    }
  }
  const groups = new Map(p.groups.map((g) => [g.id, g]));
  for (const n of t.nodes) {
    const r = byNode.get(n.id), g = groups.get(n.parent);
    assert.ok(r && g, n.id);
    assert.ok(r.x >= g.x && r.y >= g.y && r.x + r.w <= g.x + g.w && r.y + r.h <= g.y + g.h, `${n.id} outside ${n.parent}`);
  }
  return p;
}

test('ELK layout: levels, no overlap, nodes inside their subnet', async () => {
  const p = await checkLayout(net());
  assert.ok(p.width > 0 && p.height > 0);
  for (const path of p.paths) assert.ok(path.points.length >= 2, path.id);
  assert.ok(p.paths.some((x) => x.id === 'u-dev-7' && x.kind === 'unknown'));
});

test('ELK layout: a busy port as a block', async () => {
  const t = net();
  for (let i = 0; i < 9; i++) {
    t.nodes.push(dev('dev-' + (60 + i), 'subnet-2', 'cam' + i, 'online'));
    t.edges.push({ id: 'pd-' + (60 + i), kind: 'port', source: 'port-1-LAN1', target: 'dev-' + (60 + i), current: true });
  }
  const p = await checkLayout(t);
  const block = p.blocks.find((b) => b.id === 'block-port-1-LAN1');
  assert.ok(block);
  for (const id of ['dev-4', 'dev-60', 'dev-68']) {
    const r = p.nodes.get(id);
    assert.ok(r.x >= block.x && r.y >= block.y && r.x + r.w <= block.x + block.w && r.y + r.h <= block.y + block.h, id);
  }
  // Wider than tall is avoided: 11 leaves in 3 columns.
  assert.ok(block.w < 4 * SIZE.w);
});

test('ELK layout: 250 devices in under 2 seconds (SC-002)', async () => {
  const t = { groups: [{ id: 'subnet-1', label: 'big', cidr: '10.0.0.0/16', stale: false }], nodes: [], edges: [] };
  t.nodes.push(dev('dev-1', 'subnet-1', 'router', 'online', { gateway_of: ['subnet-1'] }));
  for (let p = 0; p < 5; p++) {
    t.nodes.push(port('port-1-LAN' + p, 'subnet-1', 'LAN' + p));
    t.edges.push({ id: 'rp-1-LAN' + p, kind: 'port', source: 'dev-1', target: 'port-1-LAN' + p, current: true });
  }
  for (let i = 0; i < 249; i++) {
    const id = 'dev-' + (100 + i);
    t.nodes.push(dev(id, 'subnet-1', 'd' + i, i % 7 ? 'online' : 'offline'));
    if (i % 3 === 0) t.edges.push({ id: 'pd-' + id, kind: 'port', source: 'port-1-LAN' + (i % 5), target: id, current: true });
    else if (i % 3 === 1 && i > 3) t.edges.push({ id: 'mu-' + id, kind: 'manual', source: id, target: 'dev-' + (100 + i - 3), current: true });
  }
  const start = performance.now();
  await laidOut(t);
  const ms = performance.now() - start;
  assert.ok(ms < 2000, `250-device layout took ${Math.round(ms)} ms`);
});
