// A resolved binding carries the reference the runtime returned, so an
// application can name the provider that served it (CONTRACT.md FAC-B4).
// From javascript/: node --test test/reference.test.mjs
import assert from 'node:assert/strict';
import test from 'node:test';
import {Binding, Machine} from '../index.js';

const encoder = new TextEncoder(), decoder = new TextDecoder();

const served = {provider: 'openabstractions.user-runtime', capability: 'abstraction.storage',
  contract: 'abstraction.storage/content-reader@1', guarantees: ['abstraction.storage/local-only@1'],
  scope: 'local', transport: 'oa-framed-local@1', endpoint: 'content-reader-endpoint'};

class Scripted {
  constructor(reference) { this.reference = reference; this.platform = 'linux'; }
  supports(scope, transport) { return scope === 'local' && transport === 'oa-framed-local@1'; }
  runtimeEndpoint() { return 'resolver'; }
  connect() {
    return {exchangeFrame: async (frame) => {
      assert.equal(JSON.parse(decoder.decode(frame)).service, 'abstraction.facade/resolver@1');
      return encoder.encode(JSON.stringify({version: 1, service: 'abstraction.facade/resolver@1',
        method: 'Resolve', ok: true, payload: {value: {status: 'resolved', reference: this.reference}}}));
    }, writeFrame: async () => {}};
  }
}

test('a resolved binding carries the reference the runtime returned', async () => {
  const connector = new Scripted(served);
  const binding = await new Machine('resolver', {connector}).resolveService(served.contract);
  assert.deepEqual({...binding.reference}, served);
  assert.equal(binding.endpoint, served.endpoint);
});

test('the reference of a binding is read-only', async () => {
  const connector = new Scripted(served);
  const binding = await new Machine('resolver', {connector}).resolveService(served.contract);
  assert.throws(() => { binding.reference.provider = 'someone-else'; }, TypeError);
  assert.throws(() => { binding.reference.guarantees.push('rewritten'); }, TypeError);
  assert.throws(() => { binding.reference = null; }, TypeError);
  assert.equal(binding.reference.provider, served.provider);
});

test('a new waiting policy keeps the selected reference', async () => {
  const connector = new Scripted(served);
  const binding = await new Machine('resolver', {connector}).resolveService(served.contract);
  for (const kept of [binding.callScope(), binding.withWaiting({})])
    assert.equal(kept.reference.provider, served.provider);
});

test('a binding restored from a retained endpoint carries no reference', () => {
  const restored = new Binding(new Scripted(served), 'content-reader-endpoint');
  assert.equal(restored.reference, null);
});
