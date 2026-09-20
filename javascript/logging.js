// A resolved logging sink with a bounded queue [LOG-S12, LOG-S13].
//
// write() queues a record and returns at once. One asynchronous delivery loop
// hands the queued records to the sink in order. The queue holds `capacity`
// records (1024), counting the record being delivered, and a full queue drops
// the newest record and counts it. A failed delivery keeps its record at the
// head, rebinds at once, then retries after 100 ms, doubling up to 5 s with 20 %
// jitter, rebinding before each retry. Each delivery is bounded by
// `writeTimeout` (5 s). A recovery first delivers one WARN gap record naming the
// records dropped since the last delivery. The first time the queue is empty
// after a recovery, another gap record names the records dropped after the first
// was built. Records go to no other sink.
//
// The sink emits 'failure' (error, counts), 'recovery' (counts) and 'overflow'
// (counts) once per transition. With no listener for any of them, each
// transition is one line on process.stderr.
import {EventEmitter} from 'node:events';
import {ResolutionError} from './index.js';

export const gapMessage = 'records dropped while the sink was unreachable';
const sinkContract = 'abstraction.logging/sink@1';

/** A delivery that did not settle within the write timeout. */
export class WriteTimeoutError extends Error {
  constructor(message) { super(message); this.name = 'WriteTimeoutError'; }
}

/** The default reporter's name for a failure: the resolution status, timeout or write_failed. */
export function failureClass(error) {
  if (error instanceof ResolutionError || (error?.name === 'ResolutionError' && typeof error.status === 'string')) return error.status;
  if (error instanceof WriteTimeoutError || error?.name === 'TimeoutError') return 'timeout';
  return 'write_failed';
}

/** A fixed-width UTC instant with microseconds [LOG-R7]. */
export function instant(date = new Date()) {
  return date.toISOString().replace(/\.(\d{3})Z$/, '.$1000Z');
}

const sleep = (ms, signal) => new Promise((resolve) => {
  const timer = setTimeout(() => { signal.removeEventListener('abort', done); resolve(); }, ms);
  const done = () => { clearTimeout(timer); resolve(); };
  signal.addEventListener('abort', done, {once: true});
});

function positive(name, value) {
  if (!Number.isFinite(value) || value <= 0) throw new TypeError(`${name} must be a positive number`);
}

export class ResolvedSink extends EventEmitter {
  #sink; #rebind; #options; #queue = []; #counts; #closed = false; #overflowing = false;
  // #lost: dropped records no delivered gap record reported; #reported: the
  // dropped count the latest gap record carries; #since: the current failure
  // began, or the previous gap record's until while draining; #until: the until
  // of the latest gap record built; #draining: a recovery happened and the queue
  // has not been empty since.
  #lost = 0; #reported = 0; #since = null; #until = null; #draining = false; #pending = null; #stop = new AbortController();
  #wake = null; #loop;

  /**
   * sink has write(record) returning a promise. rebind resolves the service
   * again and returns the new sink, or rejects with the resolution error.
   */
  constructor(sink, {rebind = null, capacity = 1024, writeTimeout = 5000, initialBackoff = 100, maxBackoff = 5000, program = ''} = {}) {
    super();
    if (typeof sink?.write !== 'function') throw new TypeError('sink must provide write(record)');
    if (rebind !== null && typeof rebind !== 'function') throw new TypeError('rebind must be a function');
    if (!Number.isInteger(capacity) || capacity < 1) throw new TypeError('capacity must be a positive integer');
    if (writeTimeout !== null) positive('writeTimeout', writeTimeout);
    positive('initialBackoff', initialBackoff);
    positive('maxBackoff', maxBackoff);
    if (typeof program !== 'string') throw new TypeError('program must be a string');
    this.#sink = sink;
    this.#rebind = rebind;
    this.#options = {capacity, writeTimeout, initialBackoff, maxBackoff, program};
    this.#counts = {accepted: 0, dropped: 0, written: 0, failed: 0, abandoned: 0, failing: false, lastError: ''};
    this.#loop = this.#run();
  }

  /** Resolves abstraction.logging/sink@1 now through machine and rebinds the same way. */
  static async resolve(machine, SinkClient, {guarantees = [], scope = 'local', ...options} = {}) {
    const resolve = async () => (await machine.resolveService(sinkContract, {guarantees, scope})).client(SinkClient);
    return new ResolvedSink(await resolve(), {...options, rebind: resolve});
  }

  /** A snapshot: accepted, dropped, written, failed, abandoned, queued, failing and lastError. */
  counts() { return Object.freeze({...this.#counts, queued: this.#queue.length}); }

  /** Queues the record. Returns false when it was dropped or the sink is closed. */
  write(record) {
    if (this.#closed) return false;
    if (this.#queue.length < this.#options.capacity) {
      this.#queue.push(record);
      this.#counts.accepted += 1;
      this.#notify();
      return true;
    }
    this.#counts.dropped += 1;
    this.#lost += 1;
    if (!this.#overflowing) {
      this.#overflowing = true;
      this.#transition('overflow', null, this.counts());
    }
    return false;
  }

  /** Builds a record with the writer's hop 0 claim and queues it. */
  log(level, message, attrs = {}) {
    return this.write(this.#record(level, message, attrs));
  }

  /**
   * Stops accepting records and delivers the queue until timeout milliseconds
   * pass. At the timeout the record being delivered is counted failed and the
   * records behind it abandoned. Resolves to the counts.
   */
  async close(timeout = 5000) {
    this.#closed = true;
    this.#notify();
    let timer;
    const expired = new Promise((resolve) => { timer = setTimeout(() => resolve(true), timeout); });
    const timedOut = await Promise.race([this.#loop.then(() => false), expired]);
    clearTimeout(timer);
    if (timedOut) {
      this.#stop.abort();
      this.#notify();
      await this.#loop;
    }
    return this.counts();
  }

  #record(level, message, attrs) {
    return {schema: 1n, time: instant(), level: BigInt(level), msg: message, job: '', attrs: {...attrs},
      identity: [{by: 'self', verified: false, hop: 0n, program: this.#options.program, host: '', user: '', exe: '',
        uid: -1n, gid: -1n, pid: -1n, key: '', mac: ''}]};
  }

  #notify() {
    const wake = this.#wake;
    this.#wake = null;
    if (wake) wake();
  }

  #transition(name, error, counts) {
    const listening = this.listenerCount('failure') + this.listenerCount('recovery') + this.listenerCount('overflow') > 0;
    if (listening) {
      if (name === 'failure') this.emit('failure', error, counts); else this.emit(name, counts);
      return;
    }
    const line = name === 'failure'
      ? `abstraction.logging: service unreachable (${failureClass(error)}); ${counts.queued} queued, ${counts.dropped} dropped`
      : name === 'overflow'
        ? `abstraction.logging: queue full, dropping newest records; ${counts.queued} queued, ${counts.dropped} dropped`
        : `abstraction.logging: delivering again; ${counts.dropped} dropped`;
    process.stderr.write(line + '\n');
  }

  async #run() {
    for (;;) {
      if (this.#stop.signal.aborted) {
        this.#counts.abandoned += this.#queue.length;
        this.#queue.length = 0;
        return;
      }
      try {
        await this.#reportDrained();
      } catch (error) {
        if (this.#stop.signal.aborted || !(await this.#retry(error))) {
          this.#counts.abandoned += this.#queue.length;
          this.#queue.length = 0;
          return;
        }
        continue;
      }
      if (this.#queue.length === 0) {
        if (this.#closed) return;
        const {signal} = this.#stop;
        await new Promise((resolve) => {
          const done = () => { signal.removeEventListener('abort', done); resolve(); };
          this.#wake = done;
          signal.addEventListener('abort', done, {once: true});
        });
        continue;
      }
      try {
        await this.#deliver(this.#queue[0]);
      } catch (error) {
        if (this.#stop.signal.aborted || !(await this.#retry(error))) {
          if (this.#queue.length > 0) { this.#pop(); this.#counts.failed += 1; }
          this.#counts.abandoned += this.#queue.length;
          this.#queue.length = 0;
          return;
        }
        continue;
      }
      this.#pop();
      this.#counts.written += 1;
      if (!this.#draining) this.#lost = 0;
    }
  }

  // The first time the queue is empty after a recovery, delivers a gap record for
  // the records dropped after the recovery's gap record was built [LOG-S13].
  async #reportDrained() {
    if (!this.#draining || this.#queue.length > 0) return;
    if (this.#lost === 0) {
      this.#draining = false;
      return;
    }
    await this.#deliver(this.#gapRecord());
    this.#lost -= this.#reported;
    this.#since = this.#until;
  }

  #pop() {
    this.#queue.shift();
    if (this.#queue.length === 0) this.#overflowing = false;
  }

  // One delivery, bounded by the write timeout and by close. A delivery that
  // timed out is awaited, within the next one's timeout, before the sink is
  // called again.
  async #deliver(record) {
    const {writeTimeout} = this.#options;
    const bounded = (promise, message) => {
      if (writeTimeout === null) return this.#stoppable(promise);
      let timer;
      const timeout = new Promise((_, reject) => { timer = setTimeout(() => reject(new WriteTimeoutError(message)), writeTimeout); });
      return this.#stoppable(Promise.race([promise, timeout])).finally(() => clearTimeout(timer));
    };
    if (this.#pending) {
      await bounded(this.#pending, 'abstraction.logging: delivery exceeded its write timeout: an earlier delivery has not returned');
      this.#pending = null;
    }
    const attempt = Promise.resolve().then(() => this.#sink.write(record));
    const settled = attempt.then(() => undefined, () => undefined);
    try {
      await bounded(attempt, `abstraction.logging: delivery exceeded its write timeout (${writeTimeout} ms)`);
    } catch (error) {
      if (error instanceof WriteTimeoutError || this.#stop.signal.aborted) this.#pending = settled;
      throw error;
    }
  }

  #stoppable(promise) {
    const {signal} = this.#stop;
    if (signal.aborted) return Promise.reject(new Error('abstraction.logging: sink closed'));
    return new Promise((resolve, reject) => {
      const stopped = () => reject(new Error('abstraction.logging: sink closed'));
      signal.addEventListener('abort', stopped, {once: true});
      promise.then(resolve, reject).finally(() => signal.removeEventListener('abort', stopped));
    });
  }

  async #rebindOnce() {
    if (this.#rebind === null) return;
    const sink = await this.#stoppable(Promise.resolve().then(() => this.#rebind()));
    if (typeof sink?.write !== 'function') throw new TypeError('rebind must return a sink with write(record)');
    this.#sink = sink;
  }

  async #retry(cause) {
    this.#counts.failing = true;
    this.#counts.lastError = String(cause?.message ?? cause);
    if (!this.#draining) this.#since = new Date();
    try { await this.#rebindOnce(); } catch (error) { cause = error; }
    this.#counts.lastError = String(cause?.message ?? cause);
    this.#transition('failure', cause, this.counts());
    let delay = this.#options.initialBackoff;
    for (;;) {
      await sleep(delay * (0.8 + 0.4 * Math.random()), this.#stop.signal);
      if (this.#stop.signal.aborted) return false;
      delay = Math.min(delay * 2, this.#options.maxBackoff);
      try {
        await this.#rebindOnce();
        await this.#deliver(this.#gapRecord());
      } catch (error) {
        if (this.#stop.signal.aborted) return false;
        this.#counts.lastError = String(error?.message ?? error);
        continue;
      }
      if (this.#stop.signal.aborted) return false;
      this.#counts.failing = false;
      this.#lost -= this.#reported;
      this.#since = this.#until;
      this.#draining = true;
      this.#transition('recovery', null, this.counts());
      return true;
    }
  }

  #gapRecord() {
    this.#reported = this.#lost;
    const until = new Date();
    this.#until = until;
    const record = this.#record(4, gapMessage, {dropped: String(this.#lost), since: instant(this.#since ?? until), until: instant(until)});
    record.time = instant(until);
    return record;
  }
}
