// Feature 010: tests of the Map page logic (internal/web/static/map-model.js), run in CI with
// node --test "internal/web/jstest/*.test.mjs" (research R9).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tooltip, visibleIds, details, nodeClass } from '../static/map-model.js';

// Two subnets: the router .1 (gateway of LAN, also gateway of the home subnet), a phone on LAN1,
// an offline TV on LAN1 (last known), a stale printer, DUNE with addresses in both, a tablet linked
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
      { id: 'rp-1-LAN1', kind: 'port', source: 'dev-1', target: 'port-1-LAN1', label: 'LAN1', current: true },
      { id: 'pd-4', kind: 'port', source: 'port-1-LAN1', target: 'dev-4', label: 'LAN1', current: true },
      { id: 'pd-5', kind: 'port', source: 'port-1-LAN1', target: 'dev-5', label: 'LAN1', current: false },
      { id: 'mu-8', kind: 'manual', source: 'dev-8', target: 'dev-9', current: true },
    ],
  };
}

test('tooltip', () => {
  assert.equal(tooltip(fixture().nodes[2]), 'TV · 192.168.0.5 · 00:00:5e:10:00:05 · last seen 1 day, 0 hours, 53 mins ago');
  assert.equal(tooltip({ label: '192.168.0.4', ip: '192.168.0.4', mac: '', last_seen_ago: '3 mins' }),
    '192.168.0.4 · 192.168.0.4 · last seen 3 mins ago');
});

test('visibleIds', () => {
  const t = fixture();
  const before = JSON.stringify(t);
  const all = visibleIds(t, false);
  for (const x of [...t.groups, ...t.nodes, ...t.edges]) assert.ok(all.has(x.id), x.id);

  const online = visibleIds(t, true);
  for (const id of ['dev-5', 'dev-10', 'pd-5', 'subnet-3']) assert.ok(!online.has(id), id + ' must be hidden');
  for (const id of ['dev-4', 'dev-6', 'pd-4', 'port-1-LAN1', 'rp-1-LAN1', 'subnet-1', 'subnet-2', 'mu-8', 'gw-1-subnet-1']) {
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

test('manual links', () => {
  const t = fixture();
  const rows = Object.fromEntries(details(t, 'dev-8').rows.map((r) => [r.label, r.value]));
  assert.equal(rows['Connected via'], 'AP');
  assert.equal(rows['Connected through'], undefined);
});

// Feature 011: a switch added by hand (manual), a hub behind it, wired devices behind both.
function manualFixture() {
  const dev = (id, label, state, extra = {}) => ({
    id, kind: 'device', parent: 'subnet-2', device_id: Number(id.slice(4)), label, ip: '', ips: [],
    mac: '', vendor: '', type: 'switch', state, last_seen_ago: state === 'manual' ? '' : '3 mins', gateway_of: [], ...extra,
  });
  return {
    groups: [{ id: 'subnet-2', label: '192.168.0.0/24', cidr: '192.168.0.0/24', stale: false }],
    nodes: [
      dev('dev-1', 'HG8145V5', 'online', { type: 'router', ip: '192.168.0.1', ips: ['192.168.0.1'] }),
      dev('dev-20', 'TL-SL1218P', 'manual'),
      dev('dev-21', 'hub', 'manual'),
      dev('dev-22', 'empty switch', 'manual'),
      dev('dev-4', 'pc', 'offline', { type: 'computer', ip: '192.168.0.4', ips: ['192.168.0.4'] }),
      dev('dev-5', 'nas', 'stale', { type: 'nas', ip: '192.168.0.5', ips: ['192.168.0.5'] }),
    ],
    edges: [
      { id: 'mu-20', kind: 'manual', source: 'dev-20', target: 'dev-1', current: true },
      { id: 'mu-21', kind: 'manual', source: 'dev-21', target: 'dev-20', current: true },
      { id: 'mu-4', kind: 'manual', source: 'dev-4', target: 'dev-20', current: true },
      { id: 'mu-5', kind: 'manual', source: 'dev-5', target: 'dev-21', current: true },
    ],
  };
}

test('a manual device is drawn as manual and says so', () => {
  const topo = manualFixture();
  const n = topo.nodes.find((x) => x.id === 'dev-20');
  assert.equal(nodeClass(n), 'device manual');
  assert.match(tooltip(n), /manual \(added by hand\)/);
  assert.doesNotMatch(tooltip(n), /last seen/);
  const d = details(topo, 'dev-20');
  assert.equal(d.statusText, 'manual (added by hand)');
  assert.ok(!d.rows.some((r) => r.label === 'Last seen'));
  assert.deepEqual(d.rows.find((r) => r.label === 'Connected via'), { label: 'Connected via', value: 'HG8145V5' });
});

test('Online only keeps a manual device that an online or stale device connects through', () => {
  const topo = manualFixture();
  const show = visibleIds(topo, true);
  // nas (stale) → hub → switch: both manual devices stay; the empty switch has no one behind it.
  assert.ok(show.has('dev-21') && show.has('dev-20'));
  assert.ok(!show.has('dev-22'));
  assert.ok(show.has('mu-21') && show.has('mu-20') && show.has('mu-5'));
  assert.ok(!show.has('dev-4') && !show.has('mu-4'));

  // With only the offline pc behind them, both are hidden.
  topo.nodes.find((n) => n.id === 'dev-5').state = 'offline';
  const off = visibleIds(topo, true);
  assert.ok(!off.has('dev-20') && !off.has('dev-21'));
  assert.ok(visibleIds(topo, false).has('dev-22'));
});
