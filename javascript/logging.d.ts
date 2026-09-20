import {EventEmitter} from 'node:events';
import type {Machine} from './index.js';

/** The message of the WARN record a recovery delivers. */
export declare const gapMessage: string;

/** A delivery that did not settle within the write timeout. */
export declare class WriteTimeoutError extends Error {
  constructor(message: string);
}

/** The default reporter's name for a failure: the resolution status, timeout or write_failed. */
export declare function failureClass(error: unknown): string;

/** A fixed-width UTC instant with microseconds. */
export declare function instant(date?: Date): string;

export interface SinkCounts {
  readonly accepted: number;
  readonly dropped: number;
  readonly written: number;
  readonly failed: number;
  readonly abandoned: number;
  readonly queued: number;
  readonly failing: boolean;
  readonly lastError: string;
}

export interface LogSink {
  write(record: unknown): Promise<void> | void;
}

export interface ResolvedSinkOptions {
  /** Resolves the service again and returns the new sink. */
  rebind?: (() => Promise<LogSink> | LogSink) | null;
  capacity?: number;
  /** Milliseconds; null leaves deliveries unbounded. */
  writeTimeout?: number | null;
  initialBackoff?: number;
  maxBackoff?: number;
  program?: string;
}

/**
 * A resolved logging sink with a bounded queue that retries and rebinds when
 * its service disappears. Emits 'failure' (error, counts), 'recovery' (counts)
 * and 'overflow' (counts); with no listener, each transition is one line on
 * process.stderr.
 */
export declare class ResolvedSink extends EventEmitter {
  constructor(sink: LogSink, options?: ResolvedSinkOptions);
  static resolve(machine: Machine, SinkClient: new (binding: unknown) => LogSink,
    options?: ResolvedSinkOptions & {guarantees?: string[]; scope?: 'any' | 'local' | 'remote'}): Promise<ResolvedSink>;
  counts(): SinkCounts;
  write(record: unknown): boolean;
  log(level: number, message: string, attrs?: Record<string, string>): boolean;
  close(timeout?: number): Promise<SinkCounts>;
  on(event: 'failure', listener: (error: unknown, counts: SinkCounts) => void): this;
  on(event: 'recovery' | 'overflow', listener: (counts: SinkCounts) => void): this;
}
