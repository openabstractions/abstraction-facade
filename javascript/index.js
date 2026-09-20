import {EndpointClient,RegistryClient,ResolverClient} from './js/abstraction/facade/index.mjs';
/** The status of a resolution that selected or reached no runtime. */
export const runtimeUnavailable='runtime_unavailable';
/**
 * The platform the runtime's platform declaration lists as unsupported, for a
 * Node.js `process.platform` value: `android` or `macos`; otherwise null.
 */
export function unsupportedPlatform(platform) {
  return platform==='android'?'android':platform==='darwin'?'macos':null;
}
/**
 * A resolve call produced no usable service; a transport failure is `cause`.
 * `platform` names a platform the runtime declares unsupported.
 */
export class ResolutionError extends Error {
  constructor(status,{capability=null,contract=null,lookedFor=null,platform=null,cause}={}) {
    let message=`service resolution: ${status}`;
    if(contract!==null)message+=`: ${contract} (capability ${capability})`;
    if(lookedFor!==null)message+=` at ${lookedFor}`;
    if(platform!==null)message+=`: no supported OpenAbstractions runtime exists for ${platform}`;
    super(message,cause===undefined?undefined:{cause});
    this.name='ResolutionError';this.status=status;
    this.capability=capability;this.contract=contract;this.lookedFor=lookedFor;this.platform=platform;
  }
}
const distinct = xs => Array.isArray(xs)&&xs.every(x=>typeof x==='string'&&x.length>0)&&new Set(xs).size===xs.length;
export function validateReference(request, result, lookedFor=null) {
  const ref=result.reference;
  const refused=status=>new ResolutionError(status,{capability:request.capability??null,
    contract:Array.isArray(request.contracts)?request.contracts.join(', '):null,lookedFor});
  if(result.status!=='resolved') {
    if(ref!==undefined&&ref!==null) throw refused('invalid_resolution');
    throw refused(result.status);
  }
  if(!ref || typeof ref.provider!=='string'||!ref.provider ||
      typeof ref.endpoint!=='string'||!ref.endpoint||ref.endpoint.includes('\0') ||
      ref.capability!==request.capability||!request.contracts.includes(ref.contract) ||
      !['local','remote'].includes(ref.scope)||(request.scope!=='any'&&request.scope!==ref.scope) ||
      !distinct(ref.guarantees)||!request.guarantees.every(x=>ref.guarantees.includes(x)))
    throw refused('invalid_resolution');
  return Object.freeze({...ref,guarantees:Object.freeze([...ref.guarantees])});
}
function options(value={}) {
  const result={timeout:5000,deadline:null,cancellation:null,maxFrame:1048576,...value};
  if(!Number.isFinite(result.timeout)||result.timeout<0||result.timeout>0xffffffff ||
      result.deadline!==null&&!Number.isFinite(result.deadline) ||
      !Number.isInteger(result.maxFrame)||result.maxFrame<1||result.maxFrame>2097152)
    throw new TypeError('invalid waiting or frame bounds');
  if(result.server!==undefined&&result.server!==null) {
    const s=result.server;
    if(![1,2].includes(s.principalKind)||[s.principal,s.program].some(v=>typeof v!=='string'||!v||v.includes('\0')))throw new TypeError('invalid server expectation');
    result.server=Object.freeze({principalKind:s.principalKind,principal:s.principal,program:s.program});
  }
  return Object.freeze(result);
}
/** Trusted connector supplies identity, deadline, cancellation and frame guarantees. */
export class Binding {
  constructor(connector, endpoint, waiting={}, reference=null) {
    if(typeof endpoint!=='string'||!endpoint||endpoint.includes('\0')||typeof connector.connect!=='function')
      throw new TypeError('invalid binding');
    this.connector=connector;this.endpoint=endpoint;this.waiting=options(waiting);this.reference=reference;
    Object.freeze(this);
  }
  #transport() {
    return this.connector.connect(this.endpoint,{...this.waiting,
      deadline:this.waiting.deadline??performance.now()+this.waiting.timeout});
  }
  writeFrame(frame) { return this.#transport().writeFrame(frame); }
  exchangeFrame(frame) { return this.#transport().exchangeFrame(frame); }
  callScope() {
    return new Binding(this.connector,this.endpoint,{...this.waiting,
      deadline:this.waiting.deadline??performance.now()+this.waiting.timeout},this.reference);
  }
  withWaiting({deadline=null,cancellation=null}={}) {
    return new Binding(this.connector,this.endpoint,{...this.waiting,deadline,cancellation},this.reference);
  }
  client(clientClass) { const Client = clientClass; return new Client(this); }
}
/** Marks a failure of the resolver exchange itself, as distinct from its codecs. */
class Unreached { constructor(cause) { this.cause=cause; } }
export class Machine {
  #connector;#endpoint;#waiting;#binding=null;
  /**
   * A null endpoint selects the installed runtime at each resolution: its identity
   * through `connector.selectRuntime()` unless `server` is given, then its endpoint
   * through `connector.runtimeEndpoint()`.
   */
  constructor(endpoint, {connector,...waiting}) {
    if(typeof connector?.supports!=='function')throw new TypeError('explicit trusted connector required');
    this.#connector=connector;
    if(endpoint===null||endpoint===undefined) {
      if(typeof connector.runtimeEndpoint!=='function')throw new TypeError('installed runtime selection requires connector.runtimeEndpoint()');
      this.#endpoint=null;this.#waiting=options(waiting);
    } else {
      this.#binding=new Binding(connector,endpoint,waiting);
      this.#endpoint=endpoint;this.#waiting=this.#binding.waiting;
    }
  }
  /**
   * The resolver binding. For the installed runtime it selects the runtime's identity and
   * endpoint, and rejects with ResolutionError when neither can be selected.
   */
  async resolverBinding() { return (await this.#resolver(null,null,this.#waiting.deadline??performance.now()+this.#waiting.timeout)).binding; }
  /** The resolver binding under `end`, and the waiting every binding of this resolution carries. */
  async #resolver(capability,contract,end) {
    if(this.#binding!==null)return {binding:this.#binding,waiting:this.#waiting};
    // A connector may name its platform; otherwise this process's platform decides.
    const platform=unsupportedPlatform(this.#connector.platform??globalThis.process?.platform);
    if(platform!==null)
      throw new ResolutionError(runtimeUnavailable,{capability,contract,lookedFor:'the installed runtime',platform});
    let waiting=this.#waiting;
    const unavailable=cause=>new ResolutionError(runtimeUnavailable,{capability,contract,lookedFor:'the installed runtime',cause});
    // Identity first: the resolver and the provider it names are reached only as
    // the installation declares them. A connector without selectRuntime verifies
    // identity inside connect().
    if(waiting.server==null&&typeof this.#connector.selectRuntime==='function') {
      let server;
      try { server=await this.#connector.selectRuntime({timeout:waiting.timeout,deadline:end,cancellation:waiting.cancellation}); }
      catch(error) {
        if(waiting.cancellation?.aborted)throw error;
        throw unavailable(error);
      }
      if(typeof server!=='object'||server===null)throw unavailable(new TypeError('connector selected no runtime identity'));
      waiting=options({...waiting,server});
    }
    let endpoint,cause;
    try { endpoint=this.#connector.runtimeEndpoint(); } catch(error) { cause=error; }
    if(cause===undefined&&(typeof endpoint!=='string'||!endpoint||endpoint.includes('\0')))
      cause=new TypeError('connector returned an invalid runtime endpoint');
    if(cause!==undefined)throw unavailable(cause);
    return {binding:new Binding(this.#connector,endpoint,waiting),waiting};
  }
  /** Select once. Reusable default calls each receive a fresh waiting budget. */
  async resolveService(contract,{guarantees=[],scope='any',maxFrame=this.#waiting.maxFrame}={}) {
    if(typeof contract!=='string'||!contract.includes('/')||!distinct(guarantees)||!['any','local','remote'].includes(scope))
      throw new TypeError('invalid resolution requirements');
    const capability=contract.split('/')[0];
    const request={capability,contracts:[contract],guarantees:[...guarantees],scope};
    // Selection and the resolver exchange share one budget.
    const end=this.#waiting.deadline??performance.now()+this.#waiting.timeout;
    const {binding,waiting}=await this.#resolver(capability,contract,end);
    const lookedFor=this.#endpoint===null?`the installed runtime at ${binding.endpoint}`:`the explicit endpoint ${binding.endpoint}`;
    // The resolver endpoint serves sessions (FRAMING.md "Sessions"): a connector
    // that keeps connections reuses this one for the next resolution. Provider
    // bindings below carry the caller's waiting alone.
    const resolver=new Binding(binding.connector,binding.endpoint,
      {...binding.waiting,deadline:end,cancellation:waiting.cancellation,sessions:true},binding.reference);
    const exchange={
      async exchangeFrame(frame) { try { return await resolver.exchangeFrame(frame); } catch(error) { throw new Unreached(error); } },
      async writeFrame(frame) { try { return await resolver.writeFrame(frame); } catch(error) { throw new Unreached(error); } },
    };
    let result;
    try {
      result=await new ResolverClient(exchange).resolve(request);
    } catch(error) {
      if(!(error instanceof Unreached))throw error;
      // The caller's own cancellation stays the connector's outcome.
      if(this.#waiting.cancellation?.aborted)throw error.cause;
      throw new ResolutionError(runtimeUnavailable,{capability,contract,lookedFor,cause:error.cause});
    }
    const ref=validateReference(request,result,lookedFor);
    if(!this.#connector.supports(ref.scope,ref.transport))
      throw new ResolutionError('unsupported_transport',{capability,contract,lookedFor});
    return new Binding(this.#connector,ref.endpoint,{...waiting,maxFrame},ref);
  }
  /**
   * The runtime's provider declarations, abstraction.facade/registry@1: an operator tool, whose
   * every call the runtime decides as provider.manage.
   */
  async resolveRegistry({guarantees=[],scope='any'}={}) {
    return new RegistryClient(await this.resolveService('abstraction.facade/registry@1',{guarantees,scope}));
  }
  /**
   * abstraction.facade/endpoint@1 Describe on one local endpoint: the services it hosts and each
   * one's readiness. The description's program is the provider's own claim.
   */
  async describeEndpoint(endpoint) {
    return new EndpointClient(new Binding(this.#connector,endpoint,this.#waiting)).describe();
  }
  /** One explicit budget covering resolution and all calls derived from the scope. */
  callScope() {
    const deadline=this.#waiting.deadline??performance.now()+this.#waiting.timeout;
    return new Machine(this.#endpoint,{connector:this.#connector,...this.#waiting,deadline});
  }
}
