import io
import json
import threading
from unittest.mock import patch
from abstraction.ipc import Cancellation, FrameTransport, FrameError, ServerExpectation, TIMEOUT, CANCELLED
import unittest
from types import SimpleNamespace
from abstraction.facade.jobs import Jobs, Inventory, JobError, ResultCopyError
from abstraction.facade.client import Binding
from abstraction.job.acceptance import _codec as w

class ResultTransport:
    """Immediate deterministic transport implementing composite-call scoping."""
    def __init__(self, read): self.read = read
    def call_scope(self): return ResultTransport(self.read)
    def exchange_frame(self, frame):
        request = w._service_decode(w._read_oa_service_frame, frame)
        args = w._service_decode(w._read_oa_operation_control_read_result_arguments, request.arguments, 1)
        result = w._OperationControlReadResultResult(value=self.read(args.identity, args.offset, args.max_bytes))
        payload = w._service_encode(w._write_oa_operation_control_read_result_result, result, 1)
        return w._service_encode(w._write_oa_service_reply, w._ServiceReply(version=1, service=request.service, method=request.method, ok=True, payload=payload), 0)


class ParallelNative:
    def __init__(self):
        self.events = {}
        self.released = []

    def _cancellation_create(self, out):
        handle = len(self.events) + 1
        out._obj.value = handle
        self.events[handle] = threading.Event()
        return 0

    def _cancellation_signal(self, handle):
        self.events[handle.value].set()

    def _cancellation_release(self, handle):
        self.released.append(handle.value)

class ClientControls(unittest.TestCase):
    def setUp(self):
        self.id = w.RequestIdentity(key="key", history_epoch="epoch")
        self.receipt = w.Receipt(identity=self.id, logical_owner="owner", operation_id="op",
                                 accepted_guarantees=["promise"], history_retention_ms=1)
        self.jobs = Jobs(None, expected_owner="owner", required_guarantees=["promise"])

    def test_acceptance_identity_owner_guarantees_and_unknown(self):
        for field, value in [("logical_owner","other"),("operation_id",""),
                             ("accepted_guarantees",[]),("accepted_guarantees",["promise","promise"]),("history_retention_ms",0),
                             ("identity",w.RequestIdentity(key="other",history_epoch="epoch"))]:
            receipt = w.Receipt(**vars(self.receipt));setattr(receipt,field,value)
            self.jobs._acceptance = SimpleNamespace(reconcile=lambda _:w.AcceptanceResult(outcome="accepted",receipt=receipt))
            with self.assertRaises(JobError): self.jobs.reconcile(self.id)
        self.jobs._acceptance = SimpleNamespace(reconcile=lambda _:w.AcceptanceResult(outcome="unknown"))
        self.assertEqual(self.jobs.reconcile(self.id).outcome,"unknown")
        self.jobs._acceptance = SimpleNamespace(reconcile=lambda _:w.AcceptanceResult(outcome="unknown",receipt=self.receipt))
        with self.assertRaises(JobError):self.jobs.reconcile(self.id)

    def test_permanent_failure_requires_failed_state_and_unavailable_observation(self):
        def snapshot(state, classification, cause="digest_mismatch"):
            return w.OperationSnapshot(receipt=self.receipt, state=state, progress=w.WorkProgress(done=0, total=0),
                                       cancellation_requested=False,
                                       failure=w.WorkFailure(classification=classification, message="attempt failed", cause=cause))
        for state in ("pending", "running", "transferred", "complete"):
            with self.assertRaises(JobError): self.jobs._snapshot(snapshot(state, "permanent"), self.id, ["promise"])
        self.jobs._snapshot(snapshot("failed", "permanent"), self.id, ["promise"])
        self.jobs._snapshot(snapshot("failed", "permanent", "a_future_cause"), self.id, ["promise"])
        self.jobs._snapshot(snapshot("pending", "retryable", "server_error"), self.id, ["promise"])
        self.jobs._operations = SimpleNamespace(observe_work=lambda _: w.ObservationResult(outcome="unavailable"))
        self.assertEqual(self.jobs.observe_work(self.id).outcome, "unavailable")
        retry = w.RequestIdentity(key="key", history_epoch="epoch", attempt=1)
        self.jobs._acceptance = SimpleNamespace(reconcile=lambda _: w.AcceptanceResult(outcome="accepted", receipt=self.receipt))
        with self.assertRaises(JobError): self.jobs.reconcile(retry)

    def test_submission_merges_without_mutating(self):
        sent=[]
        self.jobs._acceptance=SimpleNamespace(submit=lambda s: sent.append(s) or w.AcceptanceResult(outcome="unknown"))
        submission=w.Submission(identity=self.id,kind="download",spec=b"request",label="host · model.bin")
        self.jobs.submit(submission)
        self.assertEqual(submission.required_guarantees,[])
        self.assertEqual(sent[0].required_guarantees,["promise"])
        self.assertEqual(sent[0].label,"host · model.bin")

    def set_reader(self, read):
        self.jobs = Jobs(ResultTransport(read), expected_owner="owner", required_guarantees=["promise"])

    def chunks(self, change=None):
        def read(identity, offset, limit):
            receipt=w.Receipt(**vars(self.receipt))
            total=4
            if offset and change=="owner":receipt.logical_owner="other"
            if offset and change=="operation":receipt.operation_id="other"
            if offset and change=="total":total=5
            data=b"ab" if not offset else b"cd" if total==4 else b"cde"
            return w.ResultRead(outcome="data",chunk=w.ResultChunk(receipt=receipt,offset=offset,total=total,data=data,eof=bool(offset)))
        self.set_reader(read)

    def test_copy_changes_and_short_writes(self):
        self.chunks();out=io.BytesIO()
        self.assertEqual(self.jobs.copy_result(self.id,out),4);self.assertEqual(out.getvalue(),b"abcd")
        for change in ("owner","operation","total"):
            self.chunks(change)
            with self.assertRaises(ResultCopyError) as error:self.jobs.copy_result(self.id,io.BytesIO())
            self.assertEqual(error.exception.confirmed,2)
        self.chunks()
        with self.assertRaises(ResultCopyError) as error:self.jobs.copy_result(self.id,SimpleNamespace(write=lambda _:1))
        self.assertEqual(error.exception.confirmed,1)
        self.assertEqual(error.exception.code,"short_write")
        self.set_reader(lambda *a:w.ResultRead(outcome="not_ready"))
        with self.assertRaises(ResultCopyError) as error:self.jobs.copy_result(self.id,io.BytesIO())
        self.assertEqual(error.exception.code,"not_ready")

    def test_empty_result_and_stalled_chunk(self):
        chunk=w.ResultChunk(receipt=self.receipt,total=0,data=b"",eof=True)
        self.set_reader(lambda *a:w.ResultRead(outcome="data",chunk=chunk))
        self.assertEqual(self.jobs.copy_result(self.id,io.BytesIO()),0)
        chunk.total=1;chunk.eof=False
        with self.assertRaises(JobError):self.jobs.read_result(self.id,0,1)
        with self.assertRaises(JobError):self.jobs.read_result(self.id,0,65537)

    def test_copy_one_budget_and_fresh_subsequent_call(self):
        self.chunks()
        reply = self.jobs._transport
        clock = [100.0]
        library = object()
        token = SimpleNamespace(_library=library)
        transport = FrameTransport(library, "fixed", timeout=5, cancellation=token)
        jobs = Jobs(transport, expected_owner="owner", required_guarantees=["promise"])
        observed = []
        def call(t, frame, expects_reply):
            observed.append((t.deadline, t.cancellation, t.endpoint))
            remaining = t.timeout if t.deadline is None else t.deadline-clock[0]
            clock[0] += min(3, remaining)
            if remaining < 3: raise FrameError(TIMEOUT, "budget expired")
            return reply.exchange_frame(frame)
        with patch("abstraction.ipc.time.monotonic", side_effect=lambda:clock[0]), patch.object(FrameTransport, "_call", call):
            for start in (100.0, 200.0):
                clock[0] = start
                out = io.BytesIO()
                with self.assertRaises(ResultCopyError) as caught: jobs.copy_result(self.id, out)
                self.assertEqual(caught.exception.confirmed, 2)
                self.assertEqual(caught.exception.cause.status, TIMEOUT)
                self.assertEqual(out.getvalue(), b"ab")
                self.assertEqual(clock[0], start+5)
                self.assertEqual(observed[-2:], [(start+5, token, b"fixed")]*2)
        self.assertIsNone(transport.deadline)
        self.assertEqual(jobs.owner, "owner")
        self.assertEqual(jobs.endpoint, "fixed")

    def test_copy_frame_only_transport_refuses_before_exchange(self):
        class Frames:
            def exchange_frame(self, frame): raise AssertionError("must not exchange")
        with self.assertRaises(ResultCopyError) as caught:
            Jobs(Frames()).copy_result(self.id, io.BytesIO())
        self.assertEqual(caught.exception.code, "unsupported_waiting")
        self.assertEqual(caught.exception.confirmed, 0)

    def test_parallel_copy_orders_ranges_and_fills_short_positive_reads(self):
        body = bytes(range(256)) * 769
        native = ParallelNative()
        parent = Cancellation(native)
        proof = ServerExpectation(1, "caller-sid", "C:/trusted/server.exe")
        transport = FrameTransport(native, "fixed", timeout=5, cancellation=parent, server=proof)
        jobs = Jobs(transport, expected_owner="owner", required_guarantees=["promise"])
        third_started = threading.Event()
        seen = []

        def read(identity, offset, limit):
            if offset == 65536:
                self.assertTrue(third_started.wait(2))
            if offset == 131072:
                third_started.set()
            n = 16384 if offset == 0 else min(limit, len(body) - offset)
            data = body[offset:offset+n]
            return w.ResultRead(outcome="data", chunk=w.ResultChunk(
                receipt=self.receipt, offset=offset, total=len(body), data=data,
                eof=offset+len(data) == len(body)))

        def call(t, frame, reply):
            seen.append((t.deadline, t.cancellation, t.server, t.endpoint))
            return ResultTransport(read).exchange_frame(frame)

        with patch.object(FrameTransport, "_call", call):
            out = io.BytesIO()
            self.assertEqual(jobs.copy_result(self.id, out, max_readers=4), len(body))
        self.assertEqual(out.getvalue(), body)
        self.assertEqual(len(set(item[0] for item in seen)), 1)
        self.assertTrue(all(item[1] is not parent and item[2] is proof and item[3] == b"fixed" for item in seen))
        self.assertFalse(native.events[parent._handle.value].is_set())
        self.assertIn(2, native.released)
        parent.close()

    def test_parallel_failure_cancels_in_flight_child_and_reports_written_prefix(self):
        body = b"x" * (65536 * 3)
        native = ParallelNative()
        parent = Cancellation(native)
        jobs = Jobs(FrameTransport(native, "fixed", timeout=5, cancellation=parent),
                    expected_owner="owner", required_guarantees=["promise"])
        third_started = threading.Event()

        def call(t, frame, reply):
            request = w._service_decode(w._read_oa_service_frame, frame)
            args = w._service_decode(w._read_oa_operation_control_read_result_arguments, request.arguments, 1)
            if args.offset == 131072:
                third_started.set()
                self.assertTrue(native.events[t.cancellation._handle.value].wait(2))
                raise FrameError(CANCELLED, "read cancelled")
            if args.offset == 65536:
                self.assertTrue(third_started.wait(2))
            receipt = w.Receipt(**vars(self.receipt))
            if args.offset == 65536:
                receipt.operation_id = "other-operation"
            data = body[args.offset:args.offset+args.max_bytes]
            return ResultTransport(lambda *_: w.ResultRead(outcome="data", chunk=w.ResultChunk(
                receipt=receipt, offset=args.offset, total=len(body), data=data,
                eof=args.offset+len(data) == len(body)))).exchange_frame(frame)

        with patch.object(FrameTransport, "_call", call):
            out = io.BytesIO()
            with self.assertRaises(ResultCopyError) as caught:
                jobs.copy_result(self.id, out, max_readers=4)
        self.assertEqual(caught.exception.code, "invalid_result")
        self.assertEqual(caught.exception.confirmed, 65536)
        self.assertEqual(out.getvalue(), body[:65536])
        self.assertFalse(native.events[parent._handle.value].is_set())
        self.assertTrue(native.events[2].is_set())
        self.assertIn(2, native.released)
        parent.close()

    def test_parallel_caller_cancellation_reaches_in_flight_read(self):
        body = b"x" * (65536 * 2)
        native = ParallelNative()
        parent = Cancellation(native)
        jobs = Jobs(FrameTransport(native, "fixed", timeout=5, cancellation=parent),
                    expected_owner="owner", required_guarantees=["promise"])
        started = threading.Event()
        outcomes = []
        out = io.BytesIO()

        def call(t, frame, reply):
            request = w._service_decode(w._read_oa_service_frame, frame)
            args = w._service_decode(w._read_oa_operation_control_read_result_arguments, request.arguments, 1)
            if args.offset == 65536:
                started.set()
                self.assertTrue(native.events[t.cancellation._handle.value].wait(2))
                raise FrameError(CANCELLED, "caller cancelled")
            data = body[:65536]
            return ResultTransport(lambda *_: w.ResultRead(outcome="data", chunk=w.ResultChunk(
                receipt=self.receipt, offset=0, total=len(body), data=data, eof=False))).exchange_frame(frame)

        def copy():
            try:
                jobs.copy_result(self.id, out, max_readers=4)
            except ResultCopyError as error:
                outcomes.append(error)

        with patch.object(FrameTransport, "_call", call):
            worker = threading.Thread(target=copy)
            worker.start()
            self.assertTrue(started.wait(2))
            parent.signal()
            worker.join(2)
        self.assertFalse(worker.is_alive())
        self.assertEqual(len(outcomes), 1)
        self.assertEqual(outcomes[0].confirmed, 65536)
        self.assertEqual(outcomes[0].cause.status, CANCELLED)
        self.assertEqual(out.getvalue(), body[:65536])
        parent.close()

    def test_parallel_short_write_reports_only_confirmed_prefix(self):
        body = b"x" * (65536 * 2)
        native = ParallelNative()
        parent = Cancellation(native)
        jobs = Jobs(FrameTransport(native, "fixed", timeout=5, cancellation=parent),
                    expected_owner="owner", required_guarantees=["promise"])
        class ShortWriter:
            def __init__(self):
                self.data = bytearray()
            def write(self, data):
                n = len(data) if not self.data else 1
                self.data.extend(data[:n])
                return n
        writer = ShortWriter()
        def read(_, offset, limit):
            data = body[offset:offset+limit]
            return w.ResultRead(outcome="data", chunk=w.ResultChunk(
                receipt=self.receipt, offset=offset, total=len(body), data=data,
                eof=offset+len(data) == len(body)))
        with patch.object(FrameTransport, "_call", lambda t, frame, reply: ResultTransport(read).exchange_frame(frame)):
            with self.assertRaises(ResultCopyError) as caught:
                jobs.copy_result(self.id, writer, max_readers=4)
        self.assertEqual(caught.exception.code, "short_write")
        self.assertEqual(caught.exception.confirmed, 65537)
        self.assertEqual(bytes(writer.data), body[:65537])
        self.assertFalse(native.events[parent._handle.value].is_set())
        self.assertTrue(native.events[2].is_set())
        parent.close()

    def test_parallel_does_not_schedule_after_later_range_fails(self):
        body = b"x" * (65536 * 4)
        native = ParallelNative()
        jobs = Jobs(FrameTransport(native, "fixed", timeout=5),
                    expected_owner="owner", required_guarantees=["promise"])
        later_failed = threading.Event()
        seen = []
        def read(_, offset, limit):
            seen.append(offset)
            if offset == 65536:
                self.assertTrue(later_failed.wait(2))
            if offset == 131072:
                later_failed.set()
                raise JobError("unavailable", "later range failed")
            data = body[offset:offset+limit]
            return w.ResultRead(outcome="data", chunk=w.ResultChunk(
                receipt=self.receipt, offset=offset, total=len(body), data=data,
                eof=offset+len(data) == len(body)))
        with patch.object(FrameTransport, "_call", lambda t, frame, reply: ResultTransport(read).exchange_frame(frame)):
            out = io.BytesIO()
            with self.assertRaises(ResultCopyError) as caught:
                jobs.copy_result(self.id, out, max_readers=2)
        self.assertEqual(caught.exception.code, "unavailable")
        self.assertEqual(caught.exception.confirmed, 131072)
        self.assertEqual(out.getvalue(), body[:131072])
        self.assertNotIn(196608, seen)

    def test_parallel_custom_transport_refuses_before_read(self):
        called = []
        jobs = Jobs(ResultTransport(lambda *_: called.append(True)), expected_owner="owner")
        with self.assertRaises(ResultCopyError) as caught:
            jobs.copy_result(self.id, io.BytesIO(), max_readers=4)
        self.assertEqual(caught.exception.code, "unsupported_waiting")
        self.assertEqual(caught.exception.confirmed, 0)
        self.assertEqual(called, [])

    def test_copy_reader_count_requires_exact_bounded_integer(self):
        called = []
        jobs = Jobs(ResultTransport(lambda *_: called.append(True)), expected_owner="owner")
        for count in (True, 1.0, 0, 5, "4"):
            with self.subTest(count=count):
                with self.assertRaises(ResultCopyError) as caught:
                    jobs.copy_result(self.id, io.BytesIO(), max_readers=count)
                self.assertEqual(caught.exception.code, "invalid_request")
                self.assertEqual(caught.exception.confirmed, 0)
        self.assertEqual(called, [])

    def test_parallel_resolved_binding_keeps_server_proof(self):
        body = b"z" * (65536 + 7)
        native = ParallelNative()
        proof = ServerExpectation(1, "caller-sid", "C:/trusted/server.exe")
        jobs = Jobs(Binding(native, "fixed", timeout=5, server=proof),
                    expected_owner="owner", required_guarantees=["promise"])
        observed = []
        def read(_, offset, limit):
            data = body[offset:offset+limit]
            return w.ResultRead(outcome="data", chunk=w.ResultChunk(
                receipt=self.receipt, offset=offset, total=len(body), data=data,
                eof=offset+len(data) == len(body)))
        def call(t, frame, reply):
            observed.append((type(t), t.server, t.deadline, t.cancellation))
            return ResultTransport(read).exchange_frame(frame)
        with patch.object(FrameTransport, "_call", call):
            out = io.BytesIO()
            self.assertEqual(jobs.copy_result(self.id, out, max_readers=2), len(body))
        self.assertEqual(out.getvalue(), body)
        self.assertTrue(all(kind is Binding and server is proof and deadline is not None
                            and cancellation is not None
                            for kind, server, deadline, cancellation in observed))

    def test_inventory_does_not_expose_acceptance_or_pin_bad_page(self):
        import json
        class Reply:
            def exchange_frame(_, frame):
                request=json.loads(frame)
                result=w._JobInventoryListWorkResult(value=page)
                payload=w._service_encode(w._write_oa_job_inventory_list_work_result,result,1)
                return w._service_encode(w._write_oa_service_reply,w._ServiceReply(version=1,service=request['service'],method=request['method'],ok=True,payload=payload),0)
        inventory=Inventory(Reply(),required_guarantees=["new-provider-promise"])
        self.assertFalse(hasattr(inventory,"Submit"))
        snapshot=w.OperationSnapshot(receipt=self.receipt,state="pending",progress=w.WorkProgress(done=5,total=1))
        page=w.InventoryPage(outcome="page",snapshots=[snapshot,snapshot],next="next")
        with self.assertRaises(JobError):inventory.list_work("",2)
        self.assertEqual(inventory.owner,"")
        page.snapshots=[snapshot]
        self.assertEqual(len(inventory.list_work("",2).snapshots),1)
        self.assertEqual(inventory.owner,"owner")
        page.next="same"
        with self.assertRaises(JobError):inventory.list_work("same",2)

if __name__ == "__main__":unittest.main()
