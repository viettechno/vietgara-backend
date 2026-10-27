// Release 9.0 load test (NFR-PF-06): the repair-shop flow of a garage, driven
// against a running environment through the gateway, with the population that
// `loadseed` created. Run it with `make loadtest-*` (see README.md); the
// environment variables are listed there.
//
// Two scenarios share the garages:
//   flow    one virtual user per garage runs Quote -> Approval -> Repair Order
//           -> Close -> Settlement -> Payment -> Dashboard over and over
//   browse  staff looking things up: customer and part search, lists, the
//           dashboard. This is where most real traffic is, and what NFR-PF-01
//           (read P95 < 300 ms) is mostly about.
//
// Latency targets are k6 thresholds, so the exit code says whether the run met
// them:
//   NFR-PF-01  reads    P95 < 300 ms
//   NFR-PF-02  writes   P95 < 800 ms
//   NFR-PF-05  events   P95 < 2000 ms  (event_latency_ms, see below)
//
// Requests are tagged kind=read|write (login and polling are tagged auth and
// poll and counted in neither) and name=<verb and route without ids>.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { SharedArray } from 'k6/data';
import { Counter, Trend } from 'k6/metrics';

const API_URL = (__ENV.API_URL || 'http://localhost:18000').replace(/\/$/, '');
const PASSWORD = __ENV.LOAD_PASSWORD || 'Test#2026-local';
// The stubbed gateway trusts a client address header and, with limits at the
// production profile, needs a distinct address per virtual user. Staging sees
// the real source address.
const FORWARD_IPS = __ENV.LOAD_FORWARD_IPS === '1';
const THINK_FLOW = Number(__ENV.THINK_FLOW || 8); // seconds between two flows of a garage
const THINK_BROWSE = Number(__ENV.THINK_BROWSE || 3); // seconds between two lookups
const DURATION = __ENV.DURATION || '10m'; // steady state
const RAMP = __ENV.RAMP || '1m';
const EVENT_TIMEOUT_MS = 10000;
const POLL_MS = 250;

const owners = new SharedArray('owners', () => JSON.parse(open(__ENV.POPULATION || './population.json')).owners);
const FLOW_VUS = Number(__ENV.FLOW_VUS || owners.length);
const BROWSE_VUS = Number(__ENV.BROWSE_VUS || Math.ceil(owners.length / 2));

// Time from the moment the event's command returned to the moment its
// consumer's result is visible through the API. It includes the outbox relay's
// polling interval, so it is an upper bound of "publish to consumer completion".
const eventLatency = new Trend('event_latency_ms', true);
const eventTimeouts = new Counter('event_timeouts');
const flowsCompleted = new Counter('flows_completed');
const flowsFailed = new Counter('flows_failed');
const businessTransactions = new Counter('business_transactions');

export const options = {
  scenarios: {
    flow: {
      executor: 'ramping-vus',
      exec: 'flow',
      startVUs: 0,
      stages: [
        { duration: RAMP, target: FLOW_VUS },
        { duration: DURATION, target: FLOW_VUS },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '30s',
    },
    browse: {
      executor: 'ramping-vus',
      exec: 'browse',
      startVUs: 0,
      stages: [
        { duration: RAMP, target: BROWSE_VUS },
        { duration: DURATION, target: BROWSE_VUS },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    'http_req_duration{kind:read}': ['p(95)<300'], // NFR-PF-01
    'http_req_duration{kind:write}': ['p(95)<800'], // NFR-PF-02
    event_latency_ms: ['p(95)<2000'], // NFR-PF-05
    'event_latency_ms{event:repair_order.created}': ['max>=0'],
    'event_latency_ms{event:repair_order.closed}': ['max>=0'],
    'http_req_failed{kind:read}': ['rate<0.01'],
    'http_req_failed{kind:write}': ['rate<0.01'],
    // Sub-metrics exist only when a threshold names them; these feed the summary's counts.
    'http_reqs{kind:read}': ['count>=0'],
    'http_reqs{kind:write}': ['count>=0'],
    event_timeouts: ['count==0'],
    flows_failed: ['count==0'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

// ---- per virtual user state --------------------------------------------------

let session = null; // { token, loggedInAt, email }

function ipFor(vu, anonymous) {
  return `10.${anonymous ? 2 : 1}.${Math.floor(vu / 250) % 250}.${(vu % 250) + 1}`;
}

function headers(extra, anonymous) {
  const h = Object.assign({ 'Content-Type': 'application/json', Accept: 'application/json' }, extra || {});
  if (FORWARD_IPS) h['X-Forwarded-For'] = ipFor(__VU, anonymous);
  return h;
}

function login(owner) {
  const res = http.post(`${API_URL}/api/v1/auth/login`, JSON.stringify({ email: owner.email, password: PASSWORD }), {
    headers: headers(),
    tags: { kind: 'auth', name: 'POST auth/login' },
  });
  if (!check(res, { 'login 200': (r) => r.status === 200 })) {
    throw new Error(`sign-in of ${owner.email} failed: ${res.status} ${res.body}`);
  }
  session = { token: res.json('accessToken'), loggedInAt: Date.now(), email: owner.email };
}

// The access token lives 20 minutes; renew it well before.
function ensureSession(owner) {
  if (!session || session.email !== owner.email || Date.now() - session.loggedInAt > 12 * 60 * 1000) login(owner);
}

function uuid() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
  });
}

function pick(list) {
  return list[Math.floor(Math.random() * list.length)];
}

// call sends one request. kind is read or write; name is the route without ids.
function call(method, path, body, kind, name, opts) {
  const o = opts || {};
  const extra = Object.assign({}, o.headers || {});
  if (!o.anonymous) extra.Authorization = `Bearer ${session.token}`;
  const res = http.request(method, `${API_URL}${path}`, body === null ? null : JSON.stringify(body), {
    headers: headers(extra, o.anonymous),
    tags: { kind: o.poll ? 'poll' : kind, name: `${method} ${name}` },
    responseCallback: o.accept ? http.expectedStatuses(...o.accept) : undefined,
  });
  return res;
}

// step checks that a request succeeded (any 2xx) and returns its JSON body, or null.
function step(label, res) {
  const ok = check(res, { [`${label} 2xx`]: (r) => r.status >= 200 && r.status < 300 });
  if (!ok) {
    console.error(`${label}: ${res.status} ${String(res.body).slice(0, 200)}`);
    return null;
  }
  return res.json();
}

function today(offsetDays) {
  const d = new Date(Date.now() + offsetDays * 86400000);
  return d.toISOString().slice(0, 10);
}

// ---- the flow ------------------------------------------------------------------

export function flow() {
  const owner = owners[(__VU - 1) % owners.length];
  ensureSession(owner);
  const g = `/api/v1/garages/${owner.garageId}`;

  if (!runFlow(owner, g)) flowsFailed.add(1);
  else flowsCompleted.add(1);

  sleep(THINK_FLOW * (0.5 + Math.random()));
}

function runFlow(owner, g) {
  // Customer and vehicle: mostly a returning customer, sometimes a walk-in.
  let customerId;
  let vehicleId;
  if (Math.random() < 0.2) {
    // Unique within the run (virtual user and iteration) and across runs (the
    // clock), so two users of one garage never collide. The phone starts with 08
    // and the seeded customers' with 09.
    const stamp = String(Date.now() % 10000).padStart(4, '0');
    const c = step('create customer', call('POST', `${g}/customers`, {
      fullName: `Khach Le ${__VU}-${__ITER}-${stamp}`,
      phone: `08${String(__VU % 100).padStart(2, '0')}${String(__ITER % 10000).padStart(4, '0')}${stamp.slice(2)}`,
      personalDataConsent: true,
    }, 'write', 'garages/{id}/customers'));
    if (!c) return false;
    customerId = c.id;
    const v = step('create vehicle', call('POST', `${g}/vehicles`, {
      customerId, licensePlate: `9Z-${__VU}-${__ITER}-${stamp}`, make: 'Toyota', model: 'Vios', year: 2020,
    }, 'write', 'garages/{id}/vehicles'));
    if (!v) return false;
    vehicleId = v.id;
    businessTransactions.add(1);
  } else {
    const c = pick(owner.customers);
    customerId = c.id;
    vehicleId = pick(c.vehicleIds);
    step('open customer', call('GET', `${g}/customers/${customerId}`, null, 'read', 'garages/{id}/customers/{id}'));
  }

  // Pick a part (search, as the quote form does) and a job (the job picker).
  const part = pick(owner.parts);
  step('search parts', call('GET', `${g}/parts?query=${encodeURIComponent(part.name.split(' ')[0])}&pageSize=20`, null, 'read', 'garages/{id}/parts?query'));
  step('list jobs', call('GET', `${g}/jobs?pageSize=50`, null, 'read', 'garages/{id}/jobs'));
  const job = pick(owner.jobs);

  // Quote.
  const quote = step('create quote', call('POST', `${g}/quotes`, {
    customerId,
    vehicleId,
    approvalMode: 'QUOTE_APPROVAL_MODE_LINK',
    lines: [
      { itemType: 'QUOTE_ITEM_TYPE_LABOR', description: 'Labor', quantity: '1', unitPrice: 150000 },
      { itemType: 'QUOTE_ITEM_TYPE_PART', itemRefId: part.id, description: part.name, quantity: String(1 + Math.floor(Math.random() * 3)), unitPrice: part.salePrice },
    ],
  }, 'write', 'garages/{id}/quotes'));
  if (!quote) return false;
  businessTransactions.add(1);

  if (Math.random() < 0.5 && !step('add job to quote', call('POST', `${g}/quotes/${quote.id}/lines:fromJob`, { jobId: job.id }, 'write', 'garages/{id}/quotes/{id}/lines:fromJob'))) {
    return false;
  }

  const sent = step('send quote', call('POST', `${g}/quotes/${quote.id}/sending`, {}, 'write', 'garages/{id}/quotes/{id}/sending'));
  if (!sent) return false;

  // The customer opens the public link and approves every line (no sign-in).
  const token = sent.publicUrl.split(/[?#]/)[0].split('/').filter(Boolean).pop();
  const pub = step('open public quote', call('GET', `/api/v1/public/quotes/${token}`, null, 'read', 'public/quotes/{token}', { anonymous: true }));
  if (!pub) return false;
  const decided = step('approve quote', call('POST', `/api/v1/public/quotes/${token}/decision`, {
    decisions: pub.lines.map((l) => ({ lineId: l.id, approved: true })),
  }, 'write', 'public/quotes/{token}/decision', { anonymous: true }));
  if (!decided) return false;
  businessTransactions.add(1);

  // Repair order from the approved quote; its stock reservation is an event.
  const converted = step('convert quote', call('POST', `${g}/quotes/${quote.id}/conversion`, {}, 'write', 'garages/{id}/quotes/{id}/conversion'));
  if (!converted) return false;
  const orderId = converted.repairOrderId;
  businessTransactions.add(1);
  awaitEvent(`${g}/repair-orders/${orderId}/stock-issue`, 'repair_order.created', (b) => !!b.stockIssue);

  const order = step('open repair order', call('GET', `${g}/repair-orders/${orderId}`, null, 'read', 'garages/{id}/repair-orders/{id}'));
  if (!order) return false;
  for (const line of order.lines) {
    if (line.nonBillable) continue;
    if (!step('complete line', call('PATCH', `${g}/repair-orders/${orderId}/lines/${line.id}`, { status: 'REPAIR_ORDER_LINE_STATUS_COMPLETED' }, 'write', 'garages/{id}/repair-orders/{id}/lines/{id}'))) {
      return false;
    }
  }
  if (!step('close repair order', call('POST', `${g}/repair-orders/${orderId}/closing`, {}, 'write', 'garages/{id}/repair-orders/{id}/closing'))) return false;
  // Closing posts the stock issue: another event.
  awaitEvent(`${g}/repair-orders/${orderId}/stock-issue`, 'repair_order.closed', (b) => b.stockIssue && b.stockIssue.status === 'STOCK_ISSUE_STATUS_POSTED');

  // Settlement and payment (both need an idempotency key).
  const settlement = step('create settlement', call('POST', `${g}/settlements`, { repairOrderId: orderId }, 'write', 'garages/{id}/settlements', { headers: { 'Idempotency-Key': uuid() } }));
  if (!settlement) return false;
  businessTransactions.add(1);
  const paid = step('record payment', call('POST', `${g}/settlements/${settlement.id}/payments`, {
    amount: settlement.totalAmount, method: 'PAYMENT_METHOD_CASH',
  }, 'write', 'garages/{id}/settlements/{id}/payments', { headers: { 'Idempotency-Key': uuid() } }));
  if (!paid) return false;
  businessTransactions.add(1);
  check(paid, { 'settlement is PAID': (s) => s.status === 'SETTLEMENT_STATUS_PAID' });

  // The owner's dashboard.
  return !!dashboard(g);
}

// awaitEvent polls the API until the consumer's result is visible and records
// how long it took after the command that published the event.
function awaitEvent(path, event, done) {
  const started = Date.now();
  for (;;) {
    const res = call('GET', path, null, 'read', 'garages/{id}/repair-orders/{id}/stock-issue', { poll: true, accept: [200, 404] });
    if (res.status === 200 && done(res.json())) {
      eventLatency.add(Date.now() - started, { event });
      return;
    }
    if (Date.now() - started > EVENT_TIMEOUT_MS) {
      eventTimeouts.add(1, { event });
      return;
    }
    sleep(POLL_MS / 1000);
  }
}

function dashboard(g) {
  return step('revenue report', call('GET', `${g}/reports/revenue?period=1&from=${today(-30)}&to=${today(1)}`, null, 'read', 'garages/{id}/reports/revenue'));
}

// ---- browsing ------------------------------------------------------------------

export function browse() {
  const owner = owners[(__VU * 7) % owners.length];
  ensureSession(owner);
  const g = `/api/v1/garages/${owner.garageId}`;
  const part = pick(owner.parts);
  const customer = pick(owner.customers);

  const lookups = [
    () => call('GET', `${g}/customers?pageSize=20`, null, 'read', 'garages/{id}/customers'),
    () => call('GET', `${g}/customers?query=${encodeURIComponent('Nguyen')}&pageSize=20`, null, 'read', 'garages/{id}/customers?query'),
    () => call('GET', `${g}/customers/${customer.id}`, null, 'read', 'garages/{id}/customers/{id}'),
    () => call('GET', `${g}/vehicles?customerId=${customer.id}`, null, 'read', 'garages/{id}/vehicles?customerId'),
    () => call('GET', `${g}/parts?pageSize=20`, null, 'read', 'garages/{id}/parts'),
    () => call('GET', `${g}/parts?query=${encodeURIComponent(part.name.split(' ')[0])}&pageSize=20`, null, 'read', 'garages/{id}/parts?query'),
    () => call('GET', `${g}/stock-levels?pageSize=20`, null, 'read', 'garages/{id}/stock-levels'),
    () => call('GET', `${g}/jobs?pageSize=50`, null, 'read', 'garages/{id}/jobs'),
    () => call('GET', `${g}/quotes?pageSize=20`, null, 'read', 'garages/{id}/quotes'),
    () => call('GET', `${g}/repair-orders?pageSize=20`, null, 'read', 'garages/{id}/repair-orders'),
    () => call('GET', `${g}/settlements?pageSize=20`, null, 'read', 'garages/{id}/settlements'),
    () => call('GET', `${g}/settlements/outstanding`, null, 'read', 'garages/{id}/settlements/outstanding'),
    () => call('GET', `${g}/reports/revenue?period=1&from=${today(-30)}&to=${today(1)}`, null, 'read', 'garages/{id}/reports/revenue'),
    () => call('GET', `${g}/reports/stock-valuation`, null, 'read', 'garages/{id}/reports/stock-valuation'),
  ];

  const res = pick(lookups)();
  check(res, { 'lookup 200': (r) => r.status === 200 });
  sleep(THINK_BROWSE * (0.5 + Math.random()));
}

// ---- summary -------------------------------------------------------------------

function metric(data, name) {
  return data.metrics[name] && data.metrics[name].values;
}

function row(label, values, target) {
  if (!values) return `| ${label} | - | - | - | - | - | ${target} |\n`;
  const f = (x) => (x === undefined ? '-' : x.toFixed(0));
  return `| ${label} | ${values.count !== undefined ? values.count : '-'} | ${f(values.avg)} | ${f(values.med)} | ${f(values['p(95)'])} | ${f(values['p(99)'])} | ${target} |\n`;
}

export function handleSummary(data) {
  const count = (n) => (metric(data, n) ? metric(data, n).count : 0);
  const dir = __ENV.RESULTS_DIR || 'results';
  const reads = Object.assign({ count: count('http_reqs{kind:read}') }, metric(data, 'http_req_duration{kind:read}'));
  const writes = Object.assign({ count: count('http_reqs{kind:write}') }, metric(data, 'http_req_duration{kind:write}'));
  const created = metric(data, 'event_latency_ms{event:repair_order.created}');
  const closed = metric(data, 'event_latency_ms{event:repair_order.closed}');
  const all = metric(data, 'event_latency_ms');
  const seconds = data.state.testRunDurationMs / 1000;

  let md = `# Load test summary\n\n`;
  md += `Target ${API_URL}, ${owners.length} garages, ${FLOW_VUS} flow and ${BROWSE_VUS} browse virtual users, steady state ${DURATION}.\n\n`;
  md += `| Measure | Requests | Avg ms | Median ms | P95 ms | P99 ms | Target |\n| --- | --- | --- | --- | --- | --- | --- |\n`;
  md += row('Reads (NFR-PF-01)', reads, 'P95 < 300');
  md += row('Writes (NFR-PF-02)', writes, 'P95 < 800');
  md += row('Events, all (NFR-PF-05)', all, 'P95 < 2000');
  md += row('  repair_order.created', created, '');
  md += row('  repair_order.closed', closed, '');
  md += `\nFlows completed ${count('flows_completed')}, failed ${count('flows_failed')}; business transactions ${count('business_transactions')} `;
  md += `(${(count('business_transactions') / seconds).toFixed(2)}/s over the whole run); event timeouts ${count('event_timeouts')}; `;
  md += `HTTP failures ${(((metric(data, 'http_req_failed') || {}).rate || 0) * 100).toFixed(2)} %.\n`;

  return {
    [`${dir}/summary.json`]: JSON.stringify(data, null, 2),
    [`${dir}/summary.md`]: md,
    stdout: `\n${md}\n`,
  };
}
