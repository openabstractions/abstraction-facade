# JavaScript service bindings

The native default example uses endpoint-only compatibility selection. Configure
independent server expectations for verified local use. It does not establish
the installed-discovery trust guarantees of the Go/C++/Python defaults.


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
const machine = new Machine(connector.runtimeEndpoint(), {connector});
const binding = await machine.resolveService('abstraction.logging/sink@1');
const log = binding.client(SinkClient);
```

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

Install the actual source package containing its generated `js/` output. The
outside fixture uses an isolated package tree and installed native addon, with
an alternate pure connector test proving no native import requirement. Version
0.0.0 is development metadata. Initial behavioral proof covers logging on
Windows/MSVC; other capabilities/platforms require their own service evidence.
