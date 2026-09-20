// ResolvedSink under a sink outage [LOG-S12, LOG-S13], including the shared
// conformance/faults/sink-outage fixture every language runs.
// From javascript/: node --test test/logging.test.mjs
import assert from 'node:assert/strict';
import {existsSync, readFileSync} from 'node:fs';
import test from 'node:test';
import {ResolutionError, runtimeUnavailable} from '../index.js';
import {ResolvedSink, failureClass, gapMessage, instant} from '../logging.js';

const fixturePath = new URL('../../../../conformance/faults/sink-outage/sink-outage.json', import.meta.url);
const instantShape = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}Z$/;

async function eventually(what, condition, timeout = 10000) {
  const deadline = Date.now() + timeout;
  while (!condition()) {
    if (Date.now() > deadline) assert.fail(`timed out waiting for ${what}`);
    await new Promise((resolve) => setTimeout(resolve, 1));
  }
}

class OutageSink {
  constructor({writeError = 'down', status = runtimeUnavailable, cause = 'down'} = {}) {
    Object.assign(this, {writeError, status, cause, down: false, delivered: [], rebinds: 0, hold: null, held: false, release: null});
  }
  async write(record) {
    if (this.down) throw new Error(this.writeError);
    if (this.hold !== null && record.msg === this.hold) {
      this.hold = null;
      this.held = true;
      await this.release.promise;
    }
    this.delivered.push(record);
  }
  // A held message's next successful write waits until release.
  holdNext(message) {
    let resolve;
    const promise = new Promise((r) => { resolve = r; });
    Object.assign(this, {hold: message, release: {promise, resolve}});
  }
  rebind = async () => {
    this.rebinds += 1;
    if (this.down) {
      throw new ResolutionError(this.status, {capability: 'abstraction.logging', contract: 'abstraction.logging/sink@1',
        lookedFor: 'the installed runtime', cause: new Error(this.cause)});
    }
    return this;
  };
}

// Captures process.stderr writes while f runs.
async function withStderr(f) {
  const lines = [];
  const write = process.stderr.write;
  process.stderr.write = (chunk, ...rest) => {
    lines.push(...String(chunk).split('\n').filter(Boolean));
    return typeof rest.at(-1) === 'function' ? (rest.at(-1)(), true) : true;
  };
  try {
    return [await f(lines), lines];
  } finally {
    process.stderr.write = write;
  }
}

const countsOf = ({accepted, dropped, written, failed, abandoned, queued, failing}) =>
  ({accepted, dropped, written, failed, abandoned, queued, failing});

async function runFixture(fixture, reporter) {
  const {options, outage, steps, expect} = fixture;
  const sink = new OutageSink({writeError: outage.write_error, status: outage.rebind_error.status, cause: outage.rebind_error.cause});
  const transitions = [];
  let failure;
  const [final, lines] = await withStderr(async (lines) => {
    const handler = new ResolvedSink(sink, {rebind: sink.rebind, capacity: options.capacity, initialBackoff: options.initial_backoff_ms,
      maxBackoff: options.max_backoff_ms, program: options.program});
    if (!reporter) {
      handler.on('failure', (error) => { failure = error; transitions.push('failure'); });
      handler.on('overflow', () => transitions.push('overflow'));
      handler.on('recovery', () => transitions.push('recovery'));
    }
    const reported = () => (reporter ? lines.length : transitions.length);
    let last;
    for (const [index, step] of steps.entries()) {
      if (step.log) for (const message of step.log) handler.log(0, message);
      else if ('outage' in step) sink.down = step.outage;
      else if (step.hold) sink.holdNext(step.hold);
      else if (step.release) sink.release.resolve();
      else if (step.await === 'held') await eventually('the held delivery', () => sink.held);
      else if (step.counts) assert.deepEqual(countsOf(handler.counts()), step.counts, `step ${index}`);
      else if (step.close) last = await handler.close(5000);
      else if (step.await === 'settled') {
        await eventually('settled', () => {
          const c = handler.counts();
          return c.queued === 0 && !c.failing && c.written + c.failed + c.abandoned === c.accepted;
        });
      } else if (step.await === 'failing') await eventually('the failure report', () => handler.counts().failing && reported() >= 1);
      else if (step.await === 'retry') await eventually('a retry during the outage', () => sink.rebinds >= 2);
      else assert.fail(`step ${index} is unknown`);
    }
    return last;
  });
  assert.deepEqual(countsOf(final), expect.counts);
  assert.ok(final.lastError.includes(expect.last_error_contains), final.lastError);
  assert.deepEqual(sink.delivered.map((r) => r.msg), expect.delivered);
  const gap = sink.delivered[2];
  const want = expect.gap;
  assert.equal(gap.level, BigInt(want.level));
  assert.equal(gap.msg, want.msg);
  assert.equal(gap.identity.length, 1);
  assert.deepEqual([gap.identity[0].hop, gap.identity[0].by, gap.identity[0].program], [BigInt(want.hop), want.by, want.program]);
  for (const [key, value] of Object.entries(want.attrs)) assert.equal(gap.attrs[key], value);
  for (const key of want.timestamps) assert.match(gap.attrs[key], instantShape);
  assert.ok(gap.attrs.since <= gap.attrs.until);
  // Every drop is reported by exactly one gap record; a later gap record begins
  // where the previous one ended.
  const gaps = sink.delivered.filter((r) => r.msg === want.msg);
  assert.deepEqual(gaps.map((r) => r.attrs.dropped), expect.gaps_dropped);
  assert.equal(gaps.reduce((total, r) => total + Number(r.attrs.dropped), 0), final.dropped);
  for (const [i, r] of gaps.entries()) {
    for (const key of want.timestamps) assert.match(r.attrs[key], instantShape);
    assert.ok(r.attrs.since <= r.attrs.until);
    if (i > 0) assert.equal(r.attrs.since, gaps[i - 1].attrs.until);
  }
  if (reporter) {
    assert.deepEqual(lines, expect.reporter);
  } else {
    assert.deepEqual(lines, []);
    assert.deepEqual(transitions, expect.transitions);
    assert.equal(failureClass(failure), expect.failure_status);
  }
}

test('sink-outage fixture', {skip: !existsSync(fixturePath) && 'conformance/faults/sink-outage is not beside this package'}, async () => {
  const fixture = JSON.parse(readFileSync(fixturePath, 'utf8'));
  await runFixture(fixture, false);
  await runFixture(fixture, true);
});

test('a burst during an outage drops the newest and the gap carries the count', async () => {
  const sink = new OutageSink();
  const handler = new ResolvedSink(sink, {rebind: sink.rebind, initialBackoff: 5, maxBackoff: 20});
  let overflows = 0;
  handler.on('overflow', () => { overflows += 1; });
  handler.on('failure', () => {});
  sink.down = true;
  for (let i = 0; i < 2000; i += 1) handler.log(0, `b${String(i).padStart(4, '0')}`);
  assert.deepEqual([handler.counts().accepted, handler.counts().dropped, handler.counts().queued], [1024, 976, 1024]);
  await eventually('a retry during the outage', () => handler.counts().failing && sink.rebinds >= 2);
  sink.down = false;
  const last = await handler.close(10000);
  assert.deepEqual([last.written, last.failed, last.dropped], [1024, 0, 976]);
  assert.equal(sink.delivered.length, 1025);
  assert.deepEqual([sink.delivered[0].msg, sink.delivered[0].attrs.dropped], [gapMessage, '976']);
  assert.deepEqual([sink.delivered[1].msg, sink.delivered[1024].msg], ['b0000', 'b1023']);
  assert.equal(overflows, 1);
});

test('without listeners an outage is two lines on process.stderr', async () => {
  const sink = new OutageSink();
  sink.down = true;
  const [last, lines] = await withStderr(async () => {
    const handler = new ResolvedSink(sink, {rebind: sink.rebind, initialBackoff: 5, maxBackoff: 10});
    for (let i = 0; i < 3; i += 1) handler.log(0, `secret record ${i}`);
    await eventually('a retry', () => sink.rebinds >= 3);
    sink.down = false;
    return handler.close(5000);
  });
  assert.deepEqual(lines, [
    'abstraction.logging: service unreachable (runtime_unavailable); 3 queued, 0 dropped',
    'abstraction.logging: delivering again; 0 dropped',
  ]);
  assert.deepEqual([last.written, last.failed], [3, 0]);
});

test('close at its timeout during an outage counts the rest abandoned', async () => {
  const sink = new OutageSink();
  sink.down = true;
  const handler = new ResolvedSink(sink, {rebind: sink.rebind, initialBackoff: 5, maxBackoff: 10});
  handler.on('failure', () => {});
  for (let i = 0; i < 5; i += 1) handler.log(0, `a${i}`);
  await eventually('the failure', () => handler.counts().failing);
  const began = Date.now();
  const last = await handler.close(100);
  assert.ok(Date.now() - began < 2000);
  assert.deepEqual(countsOf(last), {accepted: 5, dropped: 0, written: 0, failed: 1, abandoned: 4, queued: 0, failing: true});
});

test('a hung sink times out and delivers once it returns', async () => {
  let open;
  const opened = new Promise((resolve) => { open = resolve; });
  const delivered = [];
  const sink = {async write(record) { await opened; delivered.push(record.msg); }};
  const handler = new ResolvedSink(sink, {writeTimeout: 30, initialBackoff: 1, maxBackoff: 2});
  let failure;
  handler.on('failure', (error) => { failure = error; });
  const latencies = [];
  for (let i = 0; i < 100; i += 1) {
    const began = performance.now();
    handler.log(0, `h${i}`);
    latencies.push(performance.now() - began);
  }
  latencies.sort((a, b) => a - b);
  assert.ok(latencies[95] < 1, `write 95th percentile ${latencies[95]} ms`);
  await eventually('the timeout failure', () => failure !== undefined);
  assert.equal(failureClass(failure), 'timeout');
  assert.deepEqual([handler.counts().failed, handler.counts().queued], [0, 100]);
  open();
  const last = await handler.close(10000);
  assert.deepEqual([last.written, last.failed, last.failing], [100, 0, false]);
});

test('ResolvedSink.resolve raises the resolution error with no runtime and rebinds through the machine', async () => {
  const refused = new ResolutionError(runtimeUnavailable, {capability: 'abstraction.logging', contract: 'abstraction.logging/sink@1', lookedFor: 'the installed runtime'});
  await assert.rejects(ResolvedSink.resolve({resolveService: async () => { throw refused; }}, class {}), (e) => e === refused);

  const sinks = [];
  let calls = 0;
  class Client { constructor(binding) { this.binding = binding; this.delivered = []; sinks.push(this); }
    async write(record) { if (this.binding.broken) throw new Error('pipe closed'); this.delivered.push(record.msg); } }
  const machine = {async resolveService(contract, {scope}) {
    calls += 1;
    assert.deepEqual([contract, scope], ['abstraction.logging/sink@1', 'local']);
    if (calls === 2) throw refused;
    return {client: (C) => new C({broken: calls === 1})};
  }};
  const handler = await ResolvedSink.resolve(machine, Client, {initialBackoff: 1, maxBackoff: 2});
  let failure;
  handler.on('failure', (error) => { failure = error; });
  handler.log(0, 'moves to the new binding');
  await eventually('delivery through the new binding', () => sinks.length === 2 && sinks[1].delivered.length === 2);
  assert.deepEqual(sinks[1].delivered, [gapMessage, 'moves to the new binding']);
  assert.equal(failureClass(failure), runtimeUnavailable);
  await handler.close(1000);
});

test('instants are fixed width', () => {
  assert.equal(instant(new Date(Date.UTC(2026, 8, 16, 1, 2, 3, 45))), '2026-09-16T01:02:03.045000Z');
});
