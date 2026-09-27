import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch
from abstraction.ipc import ServerExpectation, FrameError, CANCELLED, DISCONNECTED, IO_ERROR, TIMEOUT, UNTRUSTED
from abstraction.facade.client import Machine, ResolutionError, unsupported_platform
from abstraction.facade.jobs import Jobs
import abstraction.facade as wire


class BindingTests(unittest.TestCase):
    def test_macos_reaches_installed_selection(self):
        with patch("abstraction.facade.client.sys.platform", "darwin"):
            self.assertIsNone(unsupported_platform())

    def test_plain_string_scope_is_rejected_before_transport(self):
        machine = Machine.__new__(Machine)
        with self.assertRaisesRegex(ValueError, "invalid resolution requirements"):
            machine._bind("abstraction.logging", "abstraction.logging/sink@1", (), "local")

    def test_application_directory_retains_runtime_trust_and_local_scope(self):
        with patch("abstraction.facade.client.wire.ApplicationsClient", side_effect=lambda transport: transport):
            bound = Machine(library=self.lib).resolve_applications(scope=wire.Scope.ANY)
        self.assertIs(bound.server, self.expected)
        self.assertEqual(bound.endpoint, b"provider")
        with self.assertRaises(ResolutionError) as refused:
            Machine(library=self.lib).resolve_applications(scope=wire.Scope.REMOTE)
        self.assertEqual(refused.exception.status, "unmet_requirements")
        self.assertEqual(len(self.transports), 1)

    def setUp(self):
        self.expected = ServerExpectation(2, "1000", "/installed/runtime")
        self.selections, self.transports = [], []
        def select(**options):
            self.selections.append(options)
            return self.expected
        self.lib = SimpleNamespace(select_runtime=select, runtime_endpoint=lambda: "resolver")
        def resolver(transport):
            self.transports.append(transport)
            return SimpleNamespace(resolve=lambda request: wire.ResolveResult(status="resolved",
                reference=wire.ServiceReference(provider="catalogue-label", capability=request.capability,
                contract=request.contracts[0], guarantees=request.guarantees, scope="local",
                transport="oa-framed-local@1", endpoint="provider")))
        self.patch = patch("abstraction.facade.client.wire.ResolverClient", side_effect=resolver)
        self.patch.start()
        self.addCleanup(self.patch.stop)
        # These cases describe a supported platform wherever the suite runs.
        platform = patch("abstraction.facade.client.unsupported_platform", return_value=None)
        platform.start()
        self.addCleanup(platform.stop)

    def test_unsupported_platform_names_itself_without_loading_the_library(self):
        with patch("abstraction.facade.client.unsupported_platform", return_value="android"), \
                patch("abstraction.facade.client.Library", side_effect=AssertionError("library loaded")):
            machine = Machine()
        with self.assertRaises(ResolutionError) as caught: machine.resolve_log()
        error = caught.exception
        self.assertEqual((error.status, error.capability, error.contract, error.looked_for, error.platform),
                         ("runtime_unavailable", "abstraction.logging", "abstraction.logging/sink@1",
                          "the installed runtime", "android"))
        self.assertIsNone(error.__cause__)
        self.assertEqual(str(error), "service resolution: runtime_unavailable: abstraction.logging/sink@1 "
                                     "(capability abstraction.logging) at the installed runtime: "
                                     "no supported OpenAbstractions runtime exists for android")
        self.assertEqual((self.selections, self.transports), ([], []))

    def test_explicit_endpoint_is_not_a_platform_refusal(self):
        with patch("abstraction.facade.client.unsupported_platform", return_value="android"):
            with patch("abstraction.facade.client.wire.ResolverClient",
                       return_value=SimpleNamespace(resolve=lambda request: wire.ResolveResult(status="unavailable"))):
                with self.assertRaises(ResolutionError) as caught: Machine("explicit", self.lib).resolve_log()
        self.assertEqual((caught.exception.status, caught.exception.platform), ("unavailable", None))

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

    def test_a_resolved_binding_carries_the_reference_the_runtime_returned(self):
        binding = Machine(library=self.lib).resolve_service("abstraction.storage/content-reader@1",
                                                            guarantees=["durable@1"])
        reference = binding.reference
        self.assertEqual((reference.provider, reference.capability, reference.contract,
                          reference.guarantees, reference.scope, reference.transport, reference.endpoint),
                         ("catalogue-label", "abstraction.storage", "abstraction.storage/content-reader@1",
                          ["durable@1"], "local", "oa-framed-local@1", "provider"))

    def test_the_reference_of_a_binding_is_read_only(self):
        binding = Machine(library=self.lib).resolve_service("abstraction.storage/content-reader@1",
                                                            guarantees=["durable@1"])
        edited = binding.reference
        edited.provider, edited.guarantees[0] = "someone-else", "rewritten"
        self.assertEqual((binding.reference.provider, binding.reference.guarantees),
                         ("catalogue-label", ["durable@1"]))

    def test_a_new_waiting_policy_keeps_the_selected_reference(self):
        binding = Machine(library=self.lib).resolve_service("abstraction.storage/content-reader@1")
        for kept in (binding.with_waiting(deadline=time.monotonic() + 5), binding.call_scope()):
            self.assertEqual(kept.reference.provider, "catalogue-label")
            self.assertEqual(kept.endpoint, b"provider")

    def test_a_capability_client_names_the_provider_that_served_it(self):
        from abstraction.facade.client import reference
        jobs = Machine(library=self.lib).resolve_jobs()
        self.assertEqual(reference(jobs).provider, "catalogue-label")
        self.assertEqual(reference(jobs).contract, "abstraction.job/acceptance@1")
        self.assertIsNone(reference(object()))

    def test_resolve_service_refuses_a_contract_that_names_no_capability(self):
        machine = Machine(library=self.lib)
        for contract in ("", "abstraction.storage", "/content-reader@1", "abstraction.storage/", None):
            with self.assertRaisesRegex(ValueError, "invalid resolution requirements"):
                machine.resolve_service(contract)
        self.assertEqual(self.transports, [])

    def test_selection_refusal_does_not_contact_resolver(self):
        def refuse(**options): raise FrameError(UNTRUSTED, "no installation")
        self.lib.select_runtime = refuse
        with self.assertRaises(ResolutionError) as caught: Machine(library=self.lib).resolve_jobs()
        self.assertEqual(caught.exception.status, "runtime_unavailable")
        self.assertEqual(caught.exception.__cause__.status, UNTRUSTED)
        self.assertEqual(self.transports, [])

    def test_remote_execution_binds_local_oa_with_installed_trust(self):
        def resolve(request):
            return wire.ResolveResult(status="resolved", reference=wire.ServiceReference(
                provider="mediated", capability=request.capability, contract=request.contracts[0],
                guarantees=[], scope="remote", transport="oa-framed-local@1", endpoint="oa-local"))
        with patch("abstraction.facade.client.wire.ResolverClient", return_value=SimpleNamespace(resolve=resolve)):
            jobs = Machine(library=self.lib).resolve_jobs()
            self.assertIs(jobs._transport.server, self.expected)
            with self.assertRaises(ResolutionError) as caught:
                Machine(library=self.lib).resolve_jobs(scope=wire.Scope.LOCAL)
            self.assertEqual(caught.exception.status, "invalid_resolution")

    def test_no_runtime_installed_is_a_resolution_error(self):
        cause = FrameError(UNTRUSTED, "installed runtime selection failed")
        def refuse(**options): raise cause
        self.lib.select_runtime = refuse
        with self.assertRaises(ResolutionError) as caught: Machine(library=self.lib).resolve_log()
        error = caught.exception
        self.assertNotIsInstance(error, FrameError)
        self.assertEqual((error.status, error.capability, error.contract, error.looked_for),
                         ("runtime_unavailable", "abstraction.logging", "abstraction.logging/sink@1", "the installed runtime"))
        self.assertIs(error.__cause__, cause)
        self.assertEqual(str(error), "service resolution: runtime_unavailable: abstraction.logging/sink@1 "
                                     "(capability abstraction.logging) at the installed runtime")

    def test_adopted_logging_with_no_runtime_fails_and_substitutes_nothing(self):
        """VISION.md 2026-09-16: an adopted capability with no runtime fails with the facade's
        resolution error, visibly. "A logging seam that quietly writes to stderr instead is the
        same defect" [LOG-S11]. The Python adoption is ServiceHandler over resolve_log()."""
        import contextlib
        import io
        import logging
        from abstraction.logging.handler import ServiceHandler
        def refuse(**options): raise FrameError(UNTRUSTED, "installed runtime selection failed")
        self.lib.select_runtime = refuse
        root = logging.getLogger()
        before, stderr = list(root.handlers), io.StringIO()
        with contextlib.redirect_stderr(stderr):
            with self.assertRaises(ResolutionError) as caught:
                root.addHandler(ServiceHandler(Machine(library=self.lib).resolve_log(scope=wire.Scope.LOCAL), program="no-runtime"))
        self.assertEqual((caught.exception.status, caught.exception.contract),
                         ("runtime_unavailable", "abstraction.logging/sink@1"))
        self.assertEqual(root.handlers, before)
        self.assertEqual(stderr.getvalue(), "")
        self.assertEqual(self.transports, [])

    def test_installed_runtime_not_listening_names_its_endpoint(self):
        cause = FrameError(IO_ERROR, "IPC open failed")
        with patch("abstraction.facade.client.wire.ResolverClient",
                   return_value=SimpleNamespace(resolve=lambda request: (_ for _ in ()).throw(cause))):
            with self.assertRaises(ResolutionError) as caught: Machine(library=self.lib).resolve_config()
        error = caught.exception
        self.assertEqual((error.status, error.contract, error.looked_for),
                         ("runtime_unavailable", "abstraction.config/reader@1", "the installed runtime at resolver"))
        self.assertIs(error.__cause__, cause)

    def test_explicit_endpoint_nobody_listens_on_is_a_resolution_error(self):
        for status in (IO_ERROR, DISCONNECTED, TIMEOUT):
            with self.subTest(status=status):
                cause = FrameError(status, "IPC open failed")
                with patch("abstraction.facade.client.wire.ResolverClient",
                           return_value=SimpleNamespace(resolve=lambda request: (_ for _ in ()).throw(cause))):
                    with self.assertRaises(ResolutionError) as caught: Machine("nobody", self.lib).resolve_jobs()
                error = caught.exception
                self.assertEqual((error.status, error.capability, error.contract, error.looked_for),
                                 ("runtime_unavailable", "abstraction.job", "abstraction.job/acceptance@1",
                                  "the explicit endpoint nobody"))
                self.assertIs(error.__cause__, cause)
                self.assertEqual(self.selections, [])

    def test_runtime_without_the_service_is_a_resolution_error(self):
        with patch("abstraction.facade.client.wire.ResolverClient",
                   return_value=SimpleNamespace(resolve=lambda request: wire.ResolveResult(status="unavailable"))):
            with self.assertRaises(ResolutionError) as caught: Machine("explicit", self.lib).resolve_storage()
        error = caught.exception
        self.assertEqual((error.status, error.capability, error.contract, error.looked_for),
                         ("unavailable", "abstraction.storage", "abstraction.storage/content-reader@1",
                          "the explicit endpoint explicit"))
        self.assertIsNone(error.__cause__)

    def test_caller_cancellation_stays_a_transport_outcome(self):
        def cancelled(**options): raise FrameError(CANCELLED, "waiting cancelled")
        self.lib.select_runtime = cancelled
        with self.assertRaises(FrameError) as caught: Machine(library=self.lib).resolve_jobs()
        self.assertEqual(caught.exception.status, CANCELLED)

    def test_explicit_independent_provider_policy(self):
        other = ServerExpectation(2, "1000", "/other/runtime")
        client = Machine(library=self.lib, provider_trust=lambda ref: other).resolve_jobs()
        self.assertIs(self.transports[0].server, self.expected)
        self.assertIs(client._transport.server, other)
        with self.assertRaises(ValueError):
            Machine(library=self.lib, provider_trust=lambda ref: None).resolve_jobs()

    def test_credentials_resolve_the_holder_and_applier_contracts(self):
        import json
        import abstraction.credentials.api as credentials
        requests = []
        def resolver(transport):
            def resolve(request):
                requests.append((request.capability, request.contracts))
                return wire.ResolveResult(status="resolved", reference=wire.ServiceReference(
                    provider="catalogue-label", capability=request.capability, contract=request.contracts[0],
                    guarantees=[], scope="local", transport="oa-framed-local@1", endpoint="credentials"))
            return SimpleNamespace(resolve=resolve)
        with patch("abstraction.facade.client.wire.ResolverClient", side_effect=resolver):
            holder = Machine("explicit", self.lib).resolve_credentials()
            applier = Machine("explicit", self.lib).resolve_credentials_applier()
        self.assertIsInstance(holder, credentials.HolderClient)
        self.assertIsInstance(applier, credentials.ApplierClient)
        self.assertEqual(requests, [("abstraction.credentials", ["abstraction.credentials/holder@1"]),
                                    ("abstraction.credentials", ["abstraction.credentials/applier@1"])])
        sent = []
        page = {"outcome": "page", "limits": {"max_credentials": 64, "max_secret_bytes": 2560,
                "tombstone_retention_ms": 1, "audit_retention_ms": 1, "audit_capacity": 1,
                "secure_store": "windows-credential-manager", "supported_kinds": ["bearer", "header"]},
                "records": [], "next": "", "complete": True}
        def exchange(frame):
            sent.append(json.loads(frame))
            return json.dumps({"version": 1, "service": "abstraction.credentials/holder@1", "method": "List",
                               "ok": True, "payload": {"value": page}}).encode()
        result = credentials.HolderClient(SimpleNamespace(exchange_frame=exchange)).list(cursor="", limit=8)
        self.assertEqual(sent[0]["arguments"], {"cursor": "", "limit": 8})
        self.assertEqual((result.outcome, result.limits.secure_store, result.complete),
                         (credentials.PageOutcome.PAGE, "windows-credential-manager", True))

    def test_registry_and_endpoint_description(self):
        registry = Machine(library=self.lib).resolve_registry()
        self.assertIsInstance(registry, wire.RegistryClient)
        self.assertEqual(self.transports[-1].server, self.expected)
        described = wire.Description(outcome="described", program="fixture", version="1", services=[
            wire.ServiceState(contract="abstraction.logging/sink@1", readiness="ready", why="", guarantees=[], capabilities={})])
        seen = []
        def endpoint_client(transport):
            seen.append(transport)
            return SimpleNamespace(describe=lambda: described)
        with patch("abstraction.facade.client.wire.EndpointClient", side_effect=endpoint_client):
            self.assertIs(Machine(library=self.lib).describe_endpoint("provider-endpoint"), described)
        self.assertEqual(seen[0].endpoint, b"provider-endpoint")
        with self.assertRaises(ValueError): Machine(library=self.lib).describe_endpoint("")

    def test_resource_table_and_leases_resolve_typed_clients(self):
        import abstraction.resource as resource
        from abstraction.facade.client import reference
        requests = []
        def resolver(transport):
            def resolve(request):
                requests.append((request.capability, request.contracts))
                return wire.ResolveResult(status="resolved", reference=wire.ServiceReference(
                    provider="catalogue-label", capability=request.capability, contract=request.contracts[0],
                    guarantees=[], scope="local", transport="oa-framed-local@1", endpoint="resource"))
            return SimpleNamespace(resolve=resolve)
        with patch("abstraction.facade.client.wire.ResolverClient", side_effect=resolver):
            table = Machine(library=self.lib).resolve_resource_table()
            leases = Machine(library=self.lib).resolve_resource_leases()
        self.assertIsInstance(table, resource.TableClient)
        self.assertIsInstance(leases, resource.LeasesClient)
        self.assertEqual(requests, [("abstraction.resource", ["abstraction.resource/table@1"]),
                                    ("abstraction.resource", ["abstraction.resource/leases@1"])])
        self.assertEqual(reference(table).provider, "catalogue-label")
        self.assertEqual(reference(leases).provider, "catalogue-label")

    def test_explicit_endpoint_compatibility_and_expired_policy(self):
        client = Machine("explicit", self.lib).resolve_jobs()
        self.assertIsNone(client._transport.server)
        self.assertEqual(self.selections, [])
        with patch("abstraction.facade.client.time.monotonic", return_value=10):
            with self.assertRaises(FrameError) as caught:
                Machine("explicit", self.lib, deadline=9).resolve_jobs()
        self.assertEqual(caught.exception.status, TIMEOUT)


if __name__ == "__main__": unittest.main()
