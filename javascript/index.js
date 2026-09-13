import {ResolverClient} from './js/abstraction/facade/rec.mjs';
export class ResolutionError extends Error {
  constructor(status) { super(`service resolution: ${status}`);this.status=status; }
}
const distinct = xs => Array.isArray(xs)&&xs.every(x=>typeof x==='string'&&x.length>0)&&new Set(xs).size===xs.length;
export function validateReference(request, result) {
  const ref=result.reference;
  if(result.status!=='resolved') {
    if(ref!==undefined&&ref!==null) throw new ResolutionError('invalid_resolution');
    throw new ResolutionError(result.status);
  }
  if(!ref || typeof ref.provider!=='string'||!ref.provider ||
      typeof ref.endpoint!=='string'||!ref.endpoint||ref.endpoint.includes('\0') ||
      ref.capability!==request.capability||!request.contracts.includes(ref.contract) ||
      !['local','remote'].includes(ref.scope)||(request.scope!=='any'&&request.scope!==ref.scope) ||
      !distinct(ref.guarantees)||!request.guarantees.every(x=>ref.guarantees.includes(x)))
    throw new ResolutionError('invalid_resolution');
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
  client(Client) { return new Client(this); }
}
export class Machine {
  constructor(endpoint, {connector,...waiting}) {
    if(typeof connector?.supports!=='function')throw new TypeError('explicit trusted connector required');
    this.binding=new Binding(connector,endpoint,waiting);
  }
  /** Select once. Reusable default calls each receive a fresh waiting budget. */
  async resolveService(contract,{guarantees=[],scope='any',maxFrame=this.binding.waiting.maxFrame}={}) {
    if(typeof contract!=='string'||!contract.includes('/')||!distinct(guarantees)||!['any','local','remote'].includes(scope))
      throw new TypeError('invalid resolution requirements');
    const request={capability:contract.split('/')[0],contracts:[contract],guarantees:[...guarantees],scope};
    const result=await new ResolverClient(this.binding).Resolve(request);
    const ref=validateReference(request,result);
    if(!this.binding.connector.supports(ref.scope,ref.transport))throw new ResolutionError('unsupported_transport');
    return new Binding(this.binding.connector,ref.endpoint,{...this.binding.waiting,maxFrame},ref);
  }
  /** One explicit budget covering resolution and all calls derived from the scope. */
  callScope() {
    return new Machine(this.binding.endpoint,{connector:this.binding.connector,...this.binding.callScope().waiting});
  }
}
