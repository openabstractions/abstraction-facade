// Declarations for index.js, the pure service resolution and binding facade.
import type { Description, RegistryClient, ResolveRequest, ResolveResult, ServiceReference } from './js/abstraction/facade/index.mjs';

/** Waiting and frame bounds a machine or binding applies to each call. */
export interface WaitingOptions {
  /** Per-call budget in milliseconds when no deadline is fixed. Default 5000. */
  timeout?: number;
  /** A fixed `performance.now()` deadline shared by every call. */
  deadline?: number | null;
  cancellation?: AbortSignal | null;
  /** Largest frame in bytes, 1..2097152. Default 1048576. */
  maxFrame?: number;
  /** The server identity a verifying connector requires. */
  server?: ServerExpectation | null;
}

export interface ServerExpectation {
  principalKind: 1 | 2;
  principal: string;
  program: string;
}

/** A frame transport as a connector returns it. */
export interface FrameTransport {
  exchangeFrame(frame: Uint8Array): Promise<Uint8Array>;
  writeFrame(frame: Uint8Array): Promise<void>;
}

/** A trusted connector: it preserves identity, limits, deadlines and cancellation. */
export interface Connector {
  supports(scope: Scope, transport: string): boolean;
  connect(endpoint: string, options: Readonly<WaitingOptions & { deadline: number }>): FrameTransport;
  /** The installed runtime's resolver endpoint; required by a Machine constructed with a null endpoint. */
  runtimeEndpoint?(): string;
  /**
   * The installed runtime's identity, selected before a null-endpoint Machine contacts the runtime.
   * Every connection of that resolution carries it as `server`. A connector that verifies identity
   * inside `connect()` omits this method.
   */
  selectRuntime?(options: { timeout: number; deadline: number; cancellation: AbortSignal | null }): Promise<ServerExpectation>;
  /** The platform the installed runtime would run on, as `process.platform` names it. Defaults to `process.platform`. */
  readonly platform?: string;
}

export type Scope = 'any' | 'local' | 'remote';

/** The status of a resolution that selected or reached no runtime. */
export declare const runtimeUnavailable: 'runtime_unavailable';

/** The platform the runtime declares unsupported for a `process.platform` value: `android`, `macos` or null. */
export declare function unsupportedPlatform(platform: string | undefined): 'android' | 'macos' | null;

/**
 * A resolve call that produced no usable service. `status` is the resolver's refusal word,
 * `invalid_resolution` or `unsupported_transport` after validation, or `runtime_unavailable`
 * when no runtime could be selected or reached; the transport failure is then `cause`.
 */
export declare class ResolutionError extends Error {
  constructor(
    status: string,
    context?: {
      capability?: string | null;
      contract?: string | null;
      lookedFor?: string | null;
      platform?: string | null;
      cause?: unknown;
    }
  );
  status: string;
  /** The requested capability, such as `abstraction.job`. */
  capability: string | null;
  /** The requested contract, such as `abstraction.job/acceptance@1`. */
  contract: string | null;
  /** What resolution looked for: the installed runtime or an explicit endpoint. */
  lookedFor: string | null;
  /** For `runtime_unavailable`, a platform the runtime declares unsupported: `android` or `macos`. */
  platform: string | null;
}

export declare function validateReference(
  request: ResolveRequest,
  result: ResolveResult,
  lookedFor?: string | null
): Readonly<ServiceReference>;

/** A fixed endpoint; `client()` wraps it in a generated service client. */
export declare class Binding implements FrameTransport {
  constructor(connector: Connector, endpoint: string, waiting?: WaitingOptions, reference?: Readonly<ServiceReference> | null);
  readonly connector: Connector;
  readonly endpoint: string;
  readonly waiting: Readonly<WaitingOptions>;
  readonly reference: Readonly<ServiceReference> | null;
  exchangeFrame(frame: Uint8Array): Promise<Uint8Array>;
  writeFrame(frame: Uint8Array): Promise<void>;
  /** One budget frozen for several calls. */
  callScope(): Binding;
  withWaiting(options?: { deadline?: number | null; cancellation?: AbortSignal | null }): Binding;
  client<T>(Client: new (transport: FrameTransport) => T): T;
}

export declare class Machine {
  /**
   * A null endpoint selects the installed runtime at each resolution: its identity through
   * `connector.selectRuntime()` unless `server` is given, then its endpoint through `connector.runtimeEndpoint()`.
   */
  constructor(endpoint: string | null, options: WaitingOptions & { connector: Connector });
  /** The resolver binding; for the installed runtime it selects identity and endpoint and may reject with ResolutionError. */
  resolverBinding(): Promise<Binding>;
  /**
   * Select one provider for a contract such as `abstraction.job/acceptance@1`. Rejects with ResolutionError
   * when the runtime is absent, unreachable or refuses; the caller's own cancellation rejects with the connector's error.
   */
  resolveService(
    contract: string,
    options?: { guarantees?: string[]; scope?: Scope; maxFrame?: number }
  ): Promise<Binding>;
  /** The runtime's provider declarations, abstraction.facade/registry@1; each call is decided as provider.manage. */
  resolveRegistry(options?: { guarantees?: string[]; scope?: Scope }): Promise<RegistryClient>;
  /** abstraction.facade/endpoint@1 Describe on one local endpoint; its program is the provider's own claim. */
  describeEndpoint(endpoint: string): Promise<Description>;
  /** One explicit budget covering resolution and all calls derived from the scope. */
  callScope(): Machine;
}
