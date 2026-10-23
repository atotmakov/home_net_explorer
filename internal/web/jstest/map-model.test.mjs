// Feature 010: tests of the Map page logic (internal/web/static/map-model.js), run in CI with
// node --test "internal/web/jstest/*.test.mjs" (research R9).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { toElements, tooltip, visibleIds, details, layout } from '../static/map-model.js';

// Two subnets: the router .1 (gateway of LAN, also gateway of the home subnet), a phone on LAN1,
// an offline TV on LAN1 (last known), a stale printer, DUNE in both subnets, a tablet linked
// manually to the access point.
function fixture() {
  const dev = (id, parent, label, ip, state, extra = {}) => ({
    id, kind: 'device', parent, device_id: Number(id.slice(4)), label, ip, ips: [ip],
    mac: '00:00:5e:10:00:' + id.slice(4).padStart(2, '0'), vendor: 'Acme', type: '', state,
    last_seen_ago: '3 mins', gateway_of: [], ...extra,
  });
  return {
    groups: [
      { id: 'subnet-1', label: 'home (192.168.1.0/24)', cidr: '192.168.1.0/24', stale: false },
      { id: 'subnet-2', label: '192.168.0.0/24', cidr: '192.168.0.0/24', stale: false },
      { id: 'subnet-3', label: '192.168.8.0/24', cidr: '192.168.8.0/24', stale: true },
    ],
    nodes: [
      dev('dev-1', 'subnet-2', 'HG8145V5', '192.168.0.1', 'online', { type: 'router', gateway_of: ['subnet-2', 'subnet-1'] }),
      dev('dev-4', 'subnet-2', 'Pixel <b>10a</b>', '192.168.0.4', 'online', { type: 'phone' }),
      dev('dev-5', 'subnet-2', 'TV', '192.168.0.5', 'offline', { last_seen_ago: '1 day, 0 hours, 53 mins' }),
      dev('dev-6', 'subnet-1', 'printer', '192.168.1.6', 'stale'),
      dev('dev-7', 'subnet-2', 'DUNE', '192.168.0.28', 'online', { ips: ['192.168.0.28', '192.168.1.43'] }),
      dev('dev-8', 'subnet-2', 'tablet', '192.168.0.8', 'online'),
      dev('dev-9', 'subnet-2', 'AP', '192.168.0.9', 'online', { type: 'access_point' }),
      dev('dev-10', 'subnet-3', 'old cam', '192.168.8.10', 'offline'),
      { id: 'port-1-LAN1', kind: 'port', parent: 'subnet-2', label: 'LAN1' },
    ],
    edges: [
      { id: 'gw-1-subnet-2', kind: 'gateway', source: 'dev-1', target: 'subnet-2', current: true },
      { id: 'gw-1-subnet-1', kind: 'gateway', source: 'dev-1', target: 'subnet-1', current: true },
      { id: 'br-7-subnet-1', kind: 'bridge', source: 'dev-7', target: 'subnet-1', current: true },
      { id: 'rp-1-LAN1', kind: 'port', source: 'dev-1', target: 'port-1-LAN1', label: 'LAN1', current: true },
      { id: 'pd-4', kind: 'port', source: 'port-1-LAN1', target: 'dev-4', label: 'LAN1', current: true },
      { id: 'pd-5', kind: 'port', source: 'port-1-LAN1', target: 'dev-5', label: 'LAN1', current: false },
      { id: 'mu-8', kind: 'manual', source: 'dev-8', target: 'dev-9', current: true },
    ],
  };
}

const byId = (els) => Object.fromEntries(els.map((e) => [e.data.id, e]));
const classes = (e) => (e.classes || '').split(/\s+/).filter(Boolean);

test('toElements groups and devices', () => {
  const els = byId(toElements(fixture()));
  for (const g of ['subnet-1', 'subnet-2', 'subnet-3']) {
    assert.equal(els[g].group, 'nodes');
    assert.ok(classes(els[g]).includes('group'));
    assert.equal(els[g].data.parent, undefined);
  }
  assert.equal(els['subnet-1'].data.label, 'home (192.168.1.0/24)');
  assert.ok(classes(els['subnet-3']).includes('stale'));

  assert.equal(els['dev-4'].data.parent, 'subnet-2');
  assert.equal(els['dev-4'].data.label, 'Pixel <b>10a</b>');
  assert.ok(classes(els['dev-4']).includes('state-online'));
  assert.ok(classes(els['dev-6']).includes('state-stale'));
  assert.ok(!classes(els['dev-6']).includes('dim'));
  assert.ok(classes(els['dev-5']).includes('state-offline'));
  assert.ok(classes(els['dev-5']).includes('dim'));

  assert.equal(tooltip(fixture().nodes[2]), 'TV · 192.168.0.5 · 00:00:5e:10:00:05 · last seen 1 day, 0 hours, 53 mins ago');
  assert.equal(tooltip({ label: '192.168.0.4', ip: '192.168.0.4', mac: '', last_seen_ago: '3 mins' }),
    '192.168.0.4 · 192.168.0.4 · last seen 3 mins ago');
  assert.deepEqual(toElements({ groups: [], nodes: [], edges: [] }), []);
});

test('toElements edges', () => {
  const els = byId(toElements(fixture()));
  // A gateway of its own group is shown on the node, not as an edge into its own box.
  assert.equal(els['gw-1-subnet-2'], undefined);
  assert.ok(classes(els['dev-1']).includes('gateway'));
  assert.ok(!classes(els['dev-4']).includes('gateway'));
  // A gateway of another group is an edge.
  assert.equal(els['gw-1-subnet-1'].group, 'edges');
  assert.ok(classes(els['gw-1-subnet-1']).includes('kind-gateway'));
  assert.equal(els['gw-1-subnet-1'].data.source, 'dev-1');
  assert.equal(els['gw-1-subnet-1'].data.target, 'subnet-1');

  assert.ok(classes(els['br-7-subnet-1']).includes('kind-bridge'));
  assert.ok(classes(els['rp-1-LAN1']).includes('kind-port'));
  assert.equal(els['rp-1-LAN1'].data.label, 'LAN1');
  assert.ok(classes(els['pd-4']).includes('kind-port'));
  assert.ok(!classes(els['pd-4']).includes('last-known'));
  assert.ok(classes(els['pd-5']).includes('last-known'));
  assert.ok(classes(els['port-1-LAN1']).includes('port'));
  assert.equal(els['port-1-LAN1'].data.parent, 'subnet-2');
});

test('visibleIds', () => {
  const t = fixture();
  const before = JSON.stringify(t);
  const all = visibleIds(t, false);
  for (const x of [...t.groups, ...t.nodes, ...t.edges]) assert.ok(all.has(x.id), x.id);

  const online = visibleIds(t, true);
  for (const id of ['dev-5', 'dev-10', 'pd-5', 'subnet-3']) assert.ok(!online.has(id), id + ' must be hidden');
  for (const id of ['dev-4', 'dev-6', 'pd-4', 'port-1-LAN1', 'rp-1-LAN1', 'subnet-1', 'subnet-2', 'mu-8', 'br-7-subnet-1']) {
    assert.ok(online.has(id), id + ' must stay');
  }
  // A port left with only offline devices is hidden with its router edge.
  const t2 = fixture();
  t2.edges = t2.edges.filter((e) => e.id !== 'pd-4');
  const online2 = visibleIds(t2, true);
  assert.ok(!online2.has('port-1-LAN1'));
  assert.ok(!online2.has('rp-1-LAN1'));
  assert.equal(JSON.stringify(t), before, 'input must not be mutated');
});

test('details', () => {
  const t = fixture();
  const rows = (d) => Object.fromEntries(d.rows.map((r) => [r.label, r.value]));

  const tv = details(t, 'dev-5');
  assert.equal(tv.title, 'TV');
  assert.equal(tv.state, 'offline');
  assert.equal(tv.statusText, 'offline');
  assert.equal(tv.href, '/devices/5');
  assert.equal(rows(tv)['Connected through'], 'LAN1 on HG8145V5 (last known)');
  assert.equal(rows(tv)['Last seen'], '1 day, 0 hours, 53 mins ago');
  assert.equal(rows(tv)['Type'], undefined, 'empty values are omitted');

  const phone = details(t, 'dev-4');
  assert.equal(phone.title, 'Pixel <b>10a</b>', 'names are plain text');
  assert.equal(rows(phone)['Connected through'], 'LAN1 on HG8145V5');
  assert.equal(rows(phone)['Type'], 'phone');
  assert.equal(rows(phone)['MAC'], '00:00:5e:10:00:04');
  assert.equal(rows(phone)['Vendor'], 'Acme');

  assert.equal(details(t, 'dev-6').statusText, 'online', 'stale reads "online", as on the Devices list');
  assert.equal(rows(details(t, 'dev-7'))['Addresses'], '192.168.0.28, 192.168.1.43');
  assert.equal(rows(details(t, 'dev-1'))['Gateway of'], '192.168.0.0/24, home (192.168.1.0/24)');
  assert.equal(details(t, 'port-1-LAN1'), null);
  assert.equal(details(t, 'nope'), null);
});

// Revision 2026-10-24: a force layout made the real map unreadable (overlapping, oversized
// boxes); positions are computed here instead and drawn with Cytoscape's preset layout.
test('layout', () => {
  const t = fixture();
  const pos = layout(t);
  const nodes = t.nodes;
  for (const n of nodes) assert.ok(pos[n.id] && Number.isFinite(pos[n.id].x) && Number.isFinite(pos[n.id].y), n.id);
  const keys = new Set(nodes.map((n) => pos[n.id].x + ',' + pos[n.id].y));
  assert.equal(keys.size, nodes.length, 'no two nodes share a position');

  // Groups do not overlap: the x ranges of their nodes are disjoint (plus a gap).
  const range = (g) => {
    const xs = nodes.filter((n) => n.parent === g).map((n) => pos[n.id].x);
    return [Math.min(...xs), Math.max(...xs)];
  };
  const groups = t.groups.map((g) => g.id).filter((g) => nodes.some((n) => n.parent === g));
  for (let i = 0; i < groups.length; i++) {
    for (let j = i + 1; j < groups.length; j++) {
      const [a0, a1] = range(groups[i]);
      const [b0, b1] = range(groups[j]);
      assert.ok(a1 + 100 <= b0 || b1 + 100 <= a0, `${groups[i]} and ${groups[j]} overlap`);
    }
  }
  // The gateway is on the top row of its group; a port is below its router; the devices behind
  // a port are in its column, below it.
  const top = Math.min(...nodes.filter((n) => n.parent === 'subnet-2').map((n) => pos[n.id].y));
  assert.equal(pos['dev-1'].y, top);
  assert.ok(pos['port-1-LAN1'].y > pos['dev-1'].y);
  for (const d of ['dev-4', 'dev-5']) {
    assert.equal(pos[d].x, pos['port-1-LAN1'].x, d + ' under its port');
    assert.ok(pos[d].y > pos['port-1-LAN1'].y);
  }
  assert.deepEqual(layout({ groups: [], nodes: [], edges: [] }), {});
});

test('manual links', () => {
  const t = fixture();
  const e = byId(toElements(t))['mu-8'];
  assert.ok(classes(e).includes('kind-manual'));
  assert.equal(e.data.arrow, true);
  const rows = Object.fromEntries(details(t, 'dev-8').rows.map((r) => [r.label, r.value]));
  assert.equal(rows['Connected via'], 'AP');
  assert.equal(rows['Connected through'], undefined);
});
