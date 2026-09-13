import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch
from abstraction.ipc import ServerExpectation, FrameError, TIMEOUT, UNTRUSTED
from abstraction.facade.client import Machine
from abstraction.facade.jobs import Jobs
from abstraction.facade import rec as wire


class BindingTests(unittest.TestCase):
    def setUp(self):
        self.expected = ServerExpectation(2, "1000", "/installed/runtime")
        self.selections, self.transports = [], []
        def select(**options):
            self.selections.append(options)
            return self.expected
        self.lib = SimpleNamespace(select_runtime=select, runtime_endpoint=lambda: "resolver")
        def resolver(transport):
            self.transports.append(transport)
            return SimpleNamespace(Resolve=lambda request: wire.ResolveResult(status="resolved",
                reference=wire.ServiceReference(provider="catalogue-label", capability=request.capability,
                contract=request.contracts[0], guarantees=request.guarantees, scope="local",
                transport="oa-framed-local@1", endpoint="provider")))
        self.patch = patch("abstraction.facade.client.wire.ResolverClient", side_effect=resolver)
        self.patch.start()
        self.addCleanup(self.patch.stop)

    def test_default_resolver_provider_and_restoration_preserve_trust(self):
        end = time.monotonic() + 5
        jobs = Machine(library=self.lib, deadline=end).resolve_jobs()
        self.assertEqual(len(self.selections), 1)
        self.assertEqual(self.selections[0]["deadline"], end)
        self.assertIs(self.transports[0].server, self.expected)
        self.assertIs(jobs._transport.server, self.expected)
        self.assertIs(jobs.with_waiting()._transport.server, self.expected)
        restored = Jobs.restore_installed("provider", "owner", library=self.lib)
        self.assertIs(restored._transport.server, self.expected)
        self.assertEqual(restored.owner, "owner")

    def test_selection_refusal_does_not_contact_resolver(self):
        def refuse(**options): raise FrameError(UNTRUSTED, "no installation")
        self.lib.select_runtime = refuse
        with self.assertRaises(FrameError) as caught: Machine(library=self.lib).resolve_jobs()
        self.assertEqual(caught.exception.status, UNTRUSTED)
        self.assertEqual(self.transports, [])

    def test_explicit_independent_provider_policy(self):
        other = ServerExpectation(2, "1000", "/other/runtime")
        client = Machine(library=self.lib, provider_trust=lambda ref: other).resolve_jobs()
        self.assertIs(self.transports[0].server, self.expected)
        self.assertIs(client._transport.server, other)
        with self.assertRaises(ValueError):
            Machine(library=self.lib, provider_trust=lambda ref: None).resolve_jobs()

    def test_explicit_endpoint_compatibility_and_expired_policy(self):
        client = Machine("explicit", self.lib).resolve_jobs()
        self.assertIsNone(client._transport.server)
        self.assertEqual(self.selections, [])
        with patch("abstraction.facade.client.time.monotonic", return_value=10):
            with self.assertRaises(FrameError) as caught:
                Machine("explicit", self.lib, deadline=9).resolve_jobs()
        self.assertEqual(caught.exception.status, TIMEOUT)


if __name__ == "__main__": unittest.main()
