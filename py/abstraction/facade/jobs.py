"""Validated durable job binding. Transport cancellation changes waiting only."""
from concurrent.futures import ThreadPoolExecutor, wait
import threading
import time
from abstraction.ipc import Cancellation, FrameTransport, Library
import abstraction.job.acceptance as wire

class JobError(RuntimeError):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code

class ResultCopyError(RuntimeError):
    """confirmed bytes were written; cause retains the original typed failure."""
    def __init__(self, confirmed, cause):
        super().__init__(str(cause))
        self.confirmed, self.cause = confirmed, cause
        self.code = getattr(cause, "code", None)

def require(condition, code, message):
    if not condition:
        raise JobError(code, message)

def text(value):
    if not isinstance(value, str) or not value:
        return False
    try:
        value.encode("utf-8")
        return True
    except UnicodeError:
        return False

def guarantees(values, code="invalid_submission"):
    values = tuple(values)
    require(all(text(g) for g in values) and len(set(values)) == len(values),
            code, "empty or duplicate guarantee")
    return values

def identity(value):
    require(isinstance(value, wire.RequestIdentity) and text(value.key) and text(value.history_epoch)
            and isinstance(value.attempt, int) and value.attempt >= 0,
            "invalid_submission", "explicit key and history epoch required")

class Owner:
    def __init__(self, value):
        self.value, self.lock = value, threading.Lock()
    def bind(self, value):
        with self.lock:
            require(text(value) and (not self.value or self.value == value),
                    "invalid_acceptance", "logical owner absent or changed")
            self.value = value

class Jobs(wire.RecoverableAcceptance, wire.OperationControl):
    def __init__(self, transport, *, required_guarantees=(), expected_owner="", _owner=None):
        require(isinstance(expected_owner, str), "invalid_submission", "invalid expected owner")
        self._transport = transport
        self._required = guarantees(required_guarantees)
        self._owner = _owner if _owner is not None else Owner(expected_owner)
        self._acceptance = wire.RecoverableAcceptanceClient(transport)
        self._operations = wire.OperationControlClient(transport)

    @classmethod
    def restore(cls, endpoint, owner, *, required_guarantees=(), library=None, **waiting):
        """Use caller-retained endpoint/owner/complete requirements after restart."""
        require(text(owner), "invalid_submission", "persisted logical owner required")
        library = library if library is not None else (waiting["cancellation"]._library if waiting.get("cancellation") is not None else Library())
        return cls(FrameTransport(library, endpoint, max_frame=2*1024*1024, **waiting),
                   required_guarantees=required_guarantees, expected_owner=owner)

    @classmethod
    def restore_installed(cls, endpoint, owner, *, required_guarantees=(), library=None,
                          timeout=5.0, deadline=None, cancellation=None):
        """Restore retained work using independently selected installation trust."""
        require(text(owner), "invalid_submission", "persisted logical owner required")
        library = library if library is not None else (cancellation._library if cancellation is not None else Library())
        # Validate waiting arguments before computing a composite selection budget.
        FrameTransport(library, endpoint, timeout=timeout, deadline=deadline, cancellation=cancellation)
        end = deadline if deadline is not None else time.monotonic() + timeout
        server = library.select_runtime(timeout=timeout, deadline=end, cancellation=cancellation)
        return cls.restore(endpoint, owner, required_guarantees=required_guarantees, library=library,
                           server=server, timeout=timeout, deadline=deadline, cancellation=cancellation)

    @property
    def endpoint(self):
        return self._transport.endpoint.decode("utf-8")

    @property
    def owner(self):
        with self._owner.lock:
            return self._owner.value

    def with_waiting(self, *, timeout=None, deadline=None, cancellation=None):
        """Fresh waiting policy, same fixed endpoint and synchronized owner."""
        t = self._transport
        transport = FrameTransport(t.library, self.endpoint,
                                   timeout=t.timeout if timeout is None else timeout,
                                   deadline=deadline, cancellation=cancellation, max_frame=t.max_frame, server=t.server)
        return type(self)(transport, required_guarantees=self._required, _owner=self._owner)

    def get_history_window(self):
        result = self._acceptance.get_history_window()
        require(text(result.history_epoch) and result.minimum_retention_ms > 0 and result.result_retention_ms >= 0,
                "invalid_acceptance", "invalid history window")
        self._owner.bind(result.logical_owner)
        return result

    def _receipt(self, receipt, requested, required, *, pin=True):
        identity(requested)
        require(receipt is not None and receipt.identity.key == requested.key
                and receipt.identity.history_epoch == requested.history_epoch
                and receipt.identity.attempt == requested.attempt
                and text(receipt.operation_id) and text(receipt.logical_owner)
                and receipt.history_retention_ms > 0,
                "invalid_acceptance", "missing or mismatched receipt")
        accepted = guarantees(receipt.accepted_guarantees, "invalid_acceptance")
        require(set(required).issubset(accepted), "invalid_acceptance", "receipt weakens requirements")
        if pin:
            self._owner.bind(receipt.logical_owner)

    def _result(self, result, requested, required):
        if result.outcome == "accepted":
            self._receipt(result.receipt, requested, required)
        else:
            require(result.outcome in ("definitely_not_accepted", "unknown", "key_conflict", "forbidden", "invalid", "unavailable")
                    and result.receipt is None, "invalid_acceptance", "inconsistent acceptance response")
        return result

    def submit(self, submission):
        identity(submission.identity)
        require(text(submission.kind), "invalid_submission", "kind required")
        requested = list(guarantees(submission.required_guarantees))
        requested.extend(g for g in self._required if g not in requested)
        outgoing = wire.Submission(identity=submission.identity, kind=submission.kind,
                                   spec=submission.spec, required_guarantees=requested,
                                   label=submission.label)
        return self._result(self._acceptance.submit(outgoing), submission.identity, requested)

    def reconcile(self, requested):
        identity(requested)
        return self._result(self._acceptance.reconcile(requested), requested, self._required)

    def cancel_work(self, requested):
        identity(requested)
        result = self._acceptance.cancel_work(requested)
        require(result.outcome in tuple(wire.CancellationOutcome),
                "invalid_cancellation", "unknown cancellation outcome")
        return result

    def _snapshot(self, snapshot, requested, required, *, pin=True):
        require(snapshot is not None and snapshot.state in
                ("pending", "running", "transferred", "complete", "failed", "cancelled")
                and snapshot.progress.done >= 0 and snapshot.progress.total >= 0,
                "invalid_observation", "inconsistent progress/state")
        require(snapshot.failure is None or snapshot.failure.classification in
                ("retryable", "permanent", "unknown"), "invalid_observation", "invalid last failure")
        # JOB-A8: a permanent failure belongs to failed work. Causes are a grant vocabulary.
        require(snapshot.failure is None or snapshot.failure.classification != "permanent"
                or snapshot.state == "failed", "invalid_observation", "permanent failure on nonfailed work")
        self._receipt(snapshot.receipt, requested, required, pin=pin)

    def observe_work(self, requested):
        identity(requested)
        result = self._operations.observe_work(requested)
        if result.outcome == "observed":
            self._snapshot(result.snapshot, requested, self._required)
        else:
            require(result.outcome in ("unknown", "forbidden", "invalid", "definitely_not_accepted", "unavailable")
                    and result.snapshot is None, "invalid_observation", "inconsistent observation")
        return result

    def read_result(self, requested, offset, max_bytes):
        identity(requested)
        require(type(offset) is int and 0 <= offset < 2**63 and type(max_bytes) is int
                and 1 <= max_bytes <= 65536, "invalid_request", "invalid result range")
        result = self._operations.read_result(requested, offset, max_bytes)
        if result.outcome != "data":
            require(result.outcome in ("not_ready", "unavailable", "unsupported", "unknown", "forbidden", "invalid")
                    and result.chunk is None, "invalid_result", "inconsistent result refusal")
            return result
        c = result.chunk
        require(c is not None and c.offset == offset and c.total >= offset,
                "invalid_result", "invalid chunk offset or total")
        n = len(c.data)
        require(n <= max_bytes and n <= c.total-offset and c.eof == (n == c.total-offset)
                and (n > 0 or c.eof), "invalid_result", "invalid chunk bounds or EOF")
        self._receipt(c.receipt, requested, self._required)
        return result

    def copy_result(self, requested, destination, *, max_readers=1):
        """Copy under one call_scope() budget; preserve confirmed partial bytes.

        Injected transports must provide call_scope() with a shared deadline
        across its exchanges. The reusable binding is never mutated.
        """
        if type(max_readers) is not int or not 1 <= max_readers <= 4:
            error = JobError("invalid_request", "result readers must be 1..4")
            raise ResultCopyError(0, error) from error
        if max_readers != 1:
            return self._copy_result_parallel(requested, destination, max_readers)
        written, operation, total = 0, None, None
        try:
            scope = getattr(self._transport, "call_scope", None)
            require(callable(scope), "unsupported_waiting", "copy_result transport requires call_scope")
            scoped = Jobs(scope(), required_guarantees=self._required,
                          _owner=self._owner)
            while True:
                result = scoped.read_result(requested, written, 65536)
                require(result.outcome == "data", result.outcome, "result copy unavailable")
                c = result.chunk
                if operation is None:
                    operation, total = c.receipt.operation_id, c.total
                require(c.receipt.operation_id == operation and c.total == total,
                        "invalid_result", "result identity or total changed")
                if c.data:
                    n = destination.write(c.data)
                    require(type(n) is int and 0 <= n <= len(c.data), "invalid_writer", "invalid write count")
                    written += n
                    require(n == len(c.data), "short_write", "result writer returned a short write")
                if c.eof:
                    return written
        except Exception as error:
            raise ResultCopyError(written, error) from error

    def _copy_result_parallel(self, requested, destination, max_readers):
        """Copy with a bounded read window and one ordered writer.

        Only the native framed transport and its bindings can opt in: their
        independent views retain the same absolute deadline and server proof.
        """
        written, pool, child = 0, None, None
        active = set()
        handed_off_cleanup = False
        try:
            from abstraction.facade.client import Binding
            require(type(max_readers) is int and 2 <= max_readers <= 4,
                    "invalid_request", "parallel result readers must be 2..4")
            scope = getattr(self._transport, "call_scope", None)
            require(callable(scope), "unsupported_waiting", "copy_result transport requires call_scope")
            root = scope()
            require(type(root) in (FrameTransport, Binding) and root.deadline is not None
                    and (root.cancellation is None or isinstance(root.cancellation, Cancellation)),
                    "unsupported_waiting", "parallel result copy requires a native framed call scope")
            child = root.cancellation.child() if root.cancellation is not None else Cancellation(root.library)

            def reader():
                transport = root.with_waiting(deadline=root.deadline, cancellation=child)
                require(transport is not root and transport.deadline == root.deadline
                        and transport.cancellation is child and transport.server is root.server
                        and transport.library is root.library and transport.endpoint == root.endpoint
                        and transport.max_frame == root.max_frame and transport.sessions == root.sessions,
                        "unsupported_waiting", "parallel reader lost its waiting policy or server proof")
                return Jobs(transport, required_guarantees=self._required, _owner=self._owner)

            first_reader = reader()
            first = first_reader.read_result(requested, 0, 65536)
            require(first.outcome == "data", first.outcome, "result copy unavailable")
            chunk = first.chunk
            operation, total = chunk.receipt.operation_id, chunk.total

            def read_range(jobs, start, stop, initial=b""):
                parts = [initial]
                offset = start + len(initial)
                while offset < stop:
                    result = jobs.read_result(requested, offset, stop - offset)
                    require(result.outcome == "data", result.outcome, "result copy unavailable")
                    c = result.chunk
                    require(c.receipt.operation_id == operation and c.total == total and c.offset == offset,
                            "invalid_result", "result identity, total or offset changed")
                    require(0 < len(c.data) <= stop - offset,
                            "invalid_result", "result reader returned no progress or crossed its range")
                    parts.append(c.data)
                    offset += len(c.data)
                return b"".join(parts)

            def write(data):
                nonlocal written
                if data:
                    n = destination.write(data)
                    require(type(n) is int and 0 <= n <= len(data), "invalid_writer", "invalid write count")
                    written += n
                    require(n == len(data), "short_write", "result writer returned a short write")

            if total == 0:
                return 0
            first_end = min(65536, total)
            write(read_range(first_reader, 0, first_end, chunk.data))
            if written == total:
                return written

            local = threading.local()
            def worker(start, stop):
                if not hasattr(local, "jobs"):
                    local.jobs = reader()
                return read_range(local.jobs, start, stop)

            pool = ThreadPoolExecutor(max_workers=max_readers)
            pending = []
            next_offset = written
            for _ in range(max_readers):
                if next_offset >= total:
                    break
                stop = min(next_offset + 65536, total)
                future = pool.submit(worker, next_offset, stop)
                active.add(future)
                pending.append((next_offset, stop, future))
                next_offset = stop
            while pending:
                start, stop, future = pending.pop(0)
                data = future.result()
                active.discard(future)
                require(len(data) == stop - start, "invalid_result", "result range was incomplete")
                write(data)
                # A later range may have failed while this ordered range was
                # in flight. Observe that failure before scheduling more work.
                for other in tuple(active):
                    if other.done():
                        other.result()
                if next_offset < total:
                    stop = min(next_offset + 65536, total)
                    future = pool.submit(worker, next_offset, stop)
                    active.add(future)
                    pending.append((next_offset, stop, future))
                    next_offset = stop
            return written
        except Exception as error:
            if child is not None:
                child.signal()
            if pool is not None:
                pool.shutdown(wait=False, cancel_futures=True)
                _, unfinished = wait(active, timeout=2.0)
                if unfinished:
                    # A native cancellation should drain promptly. Keep the
                    # token alive until late calls finish without waiting past
                    # the bounded cleanup interval on this caller's thread.
                    cleanup_lock = threading.Lock()
                    cleaned = [False]
                    def close_after_drain(completed_future):
                        with cleanup_lock:
                            if not cleaned[0] and all(f.done() for f in active):
                                cleaned[0] = True
                                child.close()
                    for future in unfinished:
                        future.add_done_callback(close_after_drain)
                    handed_off_cleanup = True
            raise ResultCopyError(written, error) from error
        finally:
            if pool is not None:
                pool.shutdown(wait=False, cancel_futures=True)
            if child is not None and not handed_off_cleanup:
                child.close()

class Inventory(wire.JobInventory):
    def __init__(self, transport, *, required_guarantees=(), expected_owner="", _owner=None):
        self._binding = Jobs(transport, required_guarantees=required_guarantees,
                             expected_owner=expected_owner, _owner=_owner)
        self._transport = transport

    @property
    def endpoint(self):
        return self._binding.endpoint

    @property
    def owner(self):
        return self._binding.owner

    def with_waiting(self, **waiting):
        binding = self._binding.with_waiting(**waiting)
        return Inventory(binding._transport, required_guarantees=binding._required, _owner=binding._owner)

    def list_work(self, cursor, limit):
        require(isinstance(cursor, str) and len(cursor.encode("utf-8")) <= 128
                and type(limit) is int and 1 <= limit <= 64,
                "invalid_request", "invalid inventory cursor or limit")
        page = wire.JobInventoryClient(self._transport).list_work(cursor, limit)
        if page.outcome != "page":
            require(page.outcome in ("gap", "forbidden", "invalid", "unavailable")
                    and not page.snapshots and not page.next and not page.complete,
                    "invalid_inventory", "inconsistent inventory refusal")
            return page
        require(len(page.snapshots) <= limit and len(page.next.encode("utf-8")) <= 128
                and ((page.complete and not page.next) or
                     (not page.complete and page.next and page.next != cursor)),
                "invalid_inventory", "inconsistent inventory continuation")
        seen, owner = set(), None
        for snapshot in page.snapshots:
            # Inventory negotiation cannot impose new guarantees on older work.
            self._binding._snapshot(snapshot, snapshot.receipt.identity, (), pin=False)
            receipt = snapshot.receipt
            require(receipt.operation_id not in seen and (owner is None or owner == receipt.logical_owner),
                    "invalid_inventory", "duplicate operation or mixed owners")
            seen.add(receipt.operation_id)
            owner = receipt.logical_owner
        if owner is not None:
            self._binding._owner.bind(owner)
        return page
