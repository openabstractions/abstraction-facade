// resolveRegistry binds the resolved abstraction.facade/registry@1 endpoint, and
// describeEndpoint reads one endpoint's endpoint@1 Description through the
// connector. From javascript/: node --test test/registry.test.mjs
import assert from 'node:assert/strict';
import test from 'node:test';
import {Machine} from '../index.js';
import {RegistryClient} from '../js/abstraction/facade/index.mjs';

const encoder = new TextEncoder(), decoder = new TextDecoder();

class Scripted {
  constructor(replies) { this.replies = replies; this.connected = []; this.platform = 'linux'; }
  supports(scope, transport) { return scope === 'local' && transport === 'oa-framed-local@1'; }
  runtimeEndpoint() { return 'resolver'; }
  connect(endpoint) {
    this.connected.push(endpoint);
    return {exchangeFrame: async (frame) => {
      const service = JSON.parse(decoder.decode(frame)).service;
      return encoder.encode(JSON.stringify(this.replies[service]));
    }, writeFrame: async () => {}};
  }
}

test('resolveRegistry and describeEndpoint', async () => {
  const connector = new Scripted({
    'abstraction.facade/resolver@1': {version: 1, service: 'abstraction.facade/resolver@1', method: 'Resolve', ok: true, payload: {value: {status: 'resolved',
      reference: {provider: 'openabstractions.user-runtime', capability: 'abstraction.facade', contract: 'abstraction.facade/registry@1', guarantees: [],
        scope: 'local', transport: 'oa-framed-local@1', endpoint: 'registry-endpoint'}}}},
    'abstraction.facade/endpoint@1': {version: 1, service: 'abstraction.facade/endpoint@1', method: 'Describe', ok: true, payload: {value: {
      outcome: 'described', program: 'fixture', version: '1',
      services: [{contract: 'abstraction.logging/sink@1', readiness: 'not_ready', why: 'journal:unreadable', guarantees: [], capabilities: {}}]}}},
  });
  const machine = new Machine('resolver', {connector});
  const registry = await machine.resolveRegistry();
  assert.ok(registry instanceof RegistryClient);
  const description = await machine.describeEndpoint('provider-endpoint');
  assert.equal(description.program, 'fixture');
  assert.equal(description.services[0].contract, 'abstraction.logging/sink@1');
  assert.equal(description.services[0].why, 'journal:unreadable');
  assert.deepEqual(connector.connected, ['resolver', 'provider-endpoint']);
});
