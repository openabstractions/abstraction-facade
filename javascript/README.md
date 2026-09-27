# JavaScript service bindings

Install status: source only today, no npm package published; 0.3.0 plans an `@openabstractions` npm package, pending the npm organization and owner consent.

The pure facade package uses generated resolver codecs and a supplied connector.
It has no native or capability dependency. A connector implements
`supports(scope, transport)` and `connect(endpoint, waitingOptions)`, returning a
generated-frame transport with asynchronous `writeFrame`/`exchangeFrame` methods.
The connector is trusted to preserve identity, limits, deadlines and cancellation.

```js
import {Machine} from '@openabstractions/facade';
import {NativeConnector} from '@openabstractions/ipc';
import {SinkClient} from '@openabstractions/logging';
const connector = new NativeConnector();
const machine = new Machine(null, {connector});
const binding = await machine.resolveService('abstraction.logging/sink@1');
const log = binding.client(SinkClient);
```

`binding.reference` names the provider that answered, read-only, so an
application can say which provider served it (CONTRACT.md `FAC-B4`). It is the
resolver's frozen `ServiceReference`: `provider`, `capability`, `contract`,
`guarantees` (a guarantee is a contract-specific promise; see the
[reference vocabulary](https://openabstractions.org/reference.html#vocabulary)), `scope`, `transport` and `endpoint`. It grants nothing, and a
`Binding` constructed from a caller-retained endpoint has `null`.

```js
console.log(`reused from ${binding.reference.provider}`);
```

An application that logs keeps a `ResolvedSink` from `@openabstractions/facade/logging`
(logging CONTRACT.md `LOG-S12`, `LOG-S13`). `log` and `write` queue and return
at once. The sink retries a failed delivery with backoff, rebinds through the
machine, drops the newest record when its 1024-record queue is full, and
delivers a gap record on recovery. `counts()` reports the loss. It emits
`failure`, `recovery` and `overflow`, and with no listener each transition is
one line on `process.stderr`:

```js
import {ResolvedSink} from '@openabstractions/facade/logging';
const events = await ResolvedSink.resolve(machine, SinkClient, {program: 'app'});
events.log(0, 'service client');
```

`new Machine(null, {connector})` selects the installed runtime at each
resolution. It first calls `connector.selectRuntime()`, which for
`NativeConnector` is the shared C ABI selector the Go, C++ and Python defaults
use, then reads `connector.runtimeEndpoint()`. The resolver connection and the
selected provider's binding both carry that identity as `server`, and the native
connector refuses a peer that fails it before sending a byte. A `server` option
replaces selection. The endpoint override variable moves the endpoint and never
supplies trust. An explicit endpoint stays unverified unless `server` is given.
`machine.resolverBinding()` returns the resolver binding the same way.

A resolution that yields no service rejects with `ResolutionError`. Its `status`
is the resolver's refusal, a validation refusal, or `runtime_unavailable` when no
runtime identity or endpoint could be selected or reached. `capability`, `contract` and `lookedFor` (the installed runtime
or the explicit endpoint) name the request, and `cause` holds the connector's
error. When the caller's own cancellation signal has aborted, the connector's
error is rethrown as it is.

Resolution checks capability, contract, provider, scope, guarantees and supported
transport before creating the capability binding. Its selected endpoint stays
fixed. No automatic retry, activation or provider fallback occurs. The reference
identifies a selected provider; durable logical operation ownership comes from
the capability's receipt.

Waiting uses monotonic `performance.now()` milliseconds and AbortSignal
cancellation. Default bindings provide a fresh five-second budget for each call.
An explicit `deadline` remains fixed across resolution and calls. Obtain
`machine.callScope()` for one budget across selection and a composite operation;
`binding.callScope()` similarly freezes one budget for several capability calls.
`withWaiting()` explicitly replaces a binding's waiting policy without changing
its selection. Reusable objects are immutable. Generated protocols alone make
no claim that a provider implements every method or that one-way writes persist.

## Build from source

Place `abstraction-identity` and `abstraction-facade` checkouts beside one
another. This facade package (`package.json`, `type: "module"`) is pure
JavaScript and needs no native build; `NativeConnector` from
`@openabstractions/ipc` does, since it wraps the shared C ABI through a Node
addon built by `abstraction-identity/javascript/CMakeLists.txt` and
`addon.cpp`.

From their common parent, install the shared native library, then build the
Node addon against it and install both packages locally:

```sh
PREFIX="/absolute/writable/ipc-prefix" # on Windows use an absolute drive path
cmake -S abstraction-identity/cpp -B build/ipc -DBUILD_SHARED_LIBS=ON -DABSTRACTION_IPC_BUILD_TESTS=OFF -DCMAKE_INSTALL_PREFIX="$PREFIX"
cmake --build build/ipc --config Release
cmake --install build/ipc --config Release
cmake -S abstraction-identity/javascript -B build/ipc-node -DCMAKE_PREFIX_PATH="$PREFIX" -DNODE_INCLUDE_DIR="/path/to/node-api/headers"
cmake --build build/ipc-node --config Release
cmake --install build/ipc-node --config Release
npm install ./abstraction-identity/javascript ./abstraction-facade/javascript
```

Set `NODE_IMPORT_LIBRARY` to the matching architecture's official `node.lib`
on Windows. The outside fixture uses an isolated package tree and this
installed native addon, with an alternate pure connector test proving no
native import requirement. Initial behavioral proof covers logging on
Windows/MSVC; other capabilities/platforms require their own service
evidence.
