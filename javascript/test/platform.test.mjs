// On a platform the runtime declares unsupported, resolving through the installed
// runtime rejects with runtime_unavailable naming the platform, before the
// connector is asked for a runtime. From javascript/: node --test test/platform.test.mjs
import assert from 'node:assert/strict';
import test from 'node:test';
import {Machine, ResolutionError, runtimeUnavailable, unsupportedPlatform} from '../index.js';

const contract = 'abstraction.logging/sink@1';

class Counting {
  constructor(platform) { this.platform = platform; this.asked = 0; }
  supports() { return true; }
  runtimeEndpoint() { this.asked += 1; return 'installed-endpoint'; }
  async selectRuntime() { this.asked += 1; return {principalKind: 2, principal: '1', program: '/installed'}; }
  connect() { throw new Error('connected on an unsupported platform'); }
}

test('declared platforms', () => {
  assert.equal(unsupportedPlatform('android'), 'android');
  assert.equal(unsupportedPlatform('darwin'), null);
  assert.equal(unsupportedPlatform('linux'), null);
  assert.equal(unsupportedPlatform('win32'), null);
});

test('macOS proceeds to the shared native selector', async () => {
  const connector = new Counting('darwin');
  const error = await new Machine(null, {connector}).resolveService(contract).then(
    () => assert.fail('resolution succeeded'), (e) => e);
  assert.equal(error.platform, null);
  assert.equal(connector.asked, 2);
});

test('an unsupported platform names itself before selection', async () => {
  const connector = new Counting('android');
  const error = await new Machine(null, {connector}).resolveService(contract).then(
    () => assert.fail('resolution succeeded'), (e) => e);
  assert.ok(error instanceof ResolutionError);
  assert.equal(error.status, runtimeUnavailable);
  assert.equal(error.platform, 'android');
  assert.equal(error.lookedFor, 'the installed runtime');
  assert.equal(error.cause, undefined);
  assert.equal(error.message, 'service resolution: runtime_unavailable: abstraction.logging/sink@1 ' +
    '(capability abstraction.logging) at the installed runtime: no supported OpenAbstractions runtime exists for android');
  assert.equal(connector.asked, 0);
});

test('the process platform decides when the connector names none', async () => {
  const original = Object.getOwnPropertyDescriptor(process, 'platform');
  Object.defineProperty(process, 'platform', {...original, value: 'android'});
  try {
    const connector = new Counting(undefined);
    const error = await new Machine(null, {connector}).resolveService(contract).then(
      () => assert.fail('resolution succeeded'), (e) => e);
    assert.equal(error.platform, 'android');
    assert.equal(connector.asked, 0);
  } finally {
    Object.defineProperty(process, 'platform', original);
  }
});

test('an explicit endpoint is the application\'s choice and is not refused by platform', async () => {
  const connector = new Counting('android');
  assert.equal((await new Machine('explicit-endpoint', {connector}).resolverBinding()).endpoint, 'explicit-endpoint');
});
