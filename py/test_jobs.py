import io
import json
from unittest.mock import patch
from abstraction.ipc import FrameTransport, FrameError, TIMEOUT, CANCELLED
import unittest
from types import SimpleNamespace
from abstraction.facade.jobs import Jobs, Inventory, JobError, ResultCopyError
from abstraction.job.acceptance import rec as w

class ResultTransport:
    """Immediate deterministic transport implementing composite-call scoping."""
    def __init__(self, read): self.read = read
    def call_scope(self): return ResultTransport(self.read)
    def exchange_frame(self, frame):
        request = w._service_decode(w._decode_oaserviceframe, frame)
        args = w._service_decode(w._decode_oaoperationcontrolreadresultarguments, request.arguments, 1)
        result = w.OAOperationControlReadResultResult(value=self.read(args.identity, args.offset, args.max_bytes))
        payload = w._service_encode(w.enc_oaoperationcontrolreadresultresult, result, 1)
        return w._service_encode(w.enc_oaservicereply, w.OAServiceReply(version=1, service=request.service, method=request.method, ok=True, payload=payload), 0)

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
            self.jobs._acceptance = SimpleNamespace(Reconcile=lambda _:w.AcceptanceResult(outcome="accepted",receipt=receipt))
            with self.assertRaises(JobError): self.jobs.Reconcile(self.id)
        self.jobs._acceptance = SimpleNamespace(Reconcile=lambda _:w.AcceptanceResult(outcome="unknown"))
        self.assertEqual(self.jobs.Reconcile(self.id).outcome,"unknown")
        self.jobs._acceptance = SimpleNamespace(Reconcile=lambda _:w.AcceptanceResult(outcome="unknown",receipt=self.receipt))
        with self.assertRaises(JobError):self.jobs.Reconcile(self.id)

    def test_submission_merges_without_mutating(self):
        sent=[]
        self.jobs._acceptance=SimpleNamespace(Submit=lambda s: sent.append(s) or w.AcceptanceResult(outcome="unknown"))
        submission=w.Submission(identity=self.id,kind="download",spec=b"request")
        self.jobs.Submit(submission)
        self.assertEqual(submission.required_guarantees,[])
        self.assertEqual(sent[0].required_guarantees,["promise"])

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
        self.assertEqual(self.jobs.CopyResult(self.id,out),4);self.assertEqual(out.getvalue(),b"abcd")
        for change in ("owner","operation","total"):
            self.chunks(change)
            with self.assertRaises(ResultCopyError) as error:self.jobs.CopyResult(self.id,io.BytesIO())
            self.assertEqual(error.exception.confirmed,2)
        self.chunks()
        with self.assertRaises(ResultCopyError) as error:self.jobs.CopyResult(self.id,SimpleNamespace(write=lambda _:1))
        self.assertEqual(error.exception.confirmed,1)
        self.assertEqual(error.exception.code,"short_write")
        self.set_reader(lambda *a:w.ResultRead(outcome="not_ready"))
        with self.assertRaises(ResultCopyError) as error:self.jobs.CopyResult(self.id,io.BytesIO())
        self.assertEqual(error.exception.code,"not_ready")

    def test_empty_result_and_stalled_chunk(self):
        chunk=w.ResultChunk(receipt=self.receipt,total=0,data=b"",eof=True)
        self.set_reader(lambda *a:w.ResultRead(outcome="data",chunk=chunk))
        self.assertEqual(self.jobs.CopyResult(self.id,io.BytesIO()),0)
        chunk.total=1;chunk.eof=False
        with self.assertRaises(JobError):self.jobs.ReadResult(self.id,0,1)
        with self.assertRaises(JobError):self.jobs.ReadResult(self.id,0,65537)

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
                with self.assertRaises(ResultCopyError) as caught: jobs.CopyResult(self.id, out)
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
            Jobs(Frames()).CopyResult(self.id, io.BytesIO())
        self.assertEqual(caught.exception.code, "unsupported_waiting")
        self.assertEqual(caught.exception.confirmed, 0)

    def test_inventory_does_not_expose_acceptance_or_pin_bad_page(self):
        import json
        class Reply:
            def exchange_frame(_, frame):
                request=json.loads(frame)
                result=w.OAJobInventoryListWorkResult(value=page)
                payload=w._service_encode(w.enc_oajobinventorylistworkresult,result,1)
                return w._service_encode(w.enc_oaservicereply,w.OAServiceReply(version=1,service=request['service'],method=request['method'],ok=True,payload=payload),0)
        inventory=Inventory(Reply(),required_guarantees=["new-provider-promise"])
        self.assertFalse(hasattr(inventory,"Submit"))
        snapshot=w.OperationSnapshot(receipt=self.receipt,state="pending",progress=w.WorkProgress(done=5,total=1))
        page=w.InventoryPage(outcome="page",snapshots=[snapshot,snapshot],next="next")
        with self.assertRaises(JobError):inventory.ListWork("",2)
        self.assertEqual(inventory.owner,"")
        page.snapshots=[snapshot]
        self.assertEqual(len(inventory.ListWork("",2).snapshots),1)
        self.assertEqual(inventory.owner,"owner")
        page.next="same"
        with self.assertRaises(JobError):inventory.ListWork("same",2)

if __name__ == "__main__":unittest.main()
