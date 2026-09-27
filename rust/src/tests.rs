use super::*;
use std::{cell::RefCell, rc::Rc};
/// Each exchange the fixture saw: endpoint, deadline, cancellation byte, frame size.
type Calls = Rc<RefCell<Vec<(String, Instant, Option<u8>, usize)>>>;
#[derive(Clone)]
struct Alternate {
    calls: Calls,
    reply: Rc<Vec<u8>>,
}
impl Alternate {
    /// A supported operating system wherever the suite runs.
    const PLATFORM: &'static str = "linux";
}
impl FrameTransport for Alternate {
    type Error = ();
    fn write_frame(&self, _: &[u8]) -> Result<(), ()> {
        Ok(())
    }
    fn exchange_frame(&self, _: &[u8]) -> Result<Vec<u8>, ()> {
        Ok((*self.reply).clone())
    }
}
impl Connector for Alternate {
    type Transport = Self;
    type Cancellation = u8;
    fn supports(&self, s: wire::Scope, t: &str) -> bool {
        s == "remote" && t == "test-framed@1"
    }
    fn connect(&self, e: &str, d: Instant, c: Option<u8>, n: usize) -> Result<Self, ()> {
        self.calls.borrow_mut().push((e.into(), d, c, n));
        Ok(self.clone())
    }
    fn runtime_endpoint(&self) -> Result<String, ()> {
        Ok("resolver".into())
    }
    fn platform(&self) -> &str {
        Self::PLATFORM
    }
}
fn reference() -> wire::ServiceReference {
    wire::ServiceReference {
        capability: "abstraction.job".into(),
        contract: "abstraction.job/acceptance@1".into(),
        provider: "implementation".into(),
        endpoint: "selected".into(),
        scope: wire::Scope::Remote,
        transport: "test-framed@1".into(),
        guarantees: vec!["required".into()],
    }
}
/// A JSON string literal, escaped the way the wire requires.
fn json(s: &str) -> String {
    let mut out = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}
/// The resolver's reply frame, spelled from the contract's wire names; the
/// generated envelope codec is private to the generated crate.
fn connector(r: Option<wire::ServiceReference>, status: &str) -> Alternate {
    Alternate {
        calls: Rc::new(RefCell::new(vec![])),
        reply: Rc::new(reply_frame(r, status)),
    }
}
fn reply_frame(r: Option<wire::ServiceReference>, status: &str) -> Vec<u8> {
    let reference = match r {
        Some(r) => format!(
            ",\"reference\":{{\"provider\":{},\"capability\":{},\"contract\":{},\"guarantees\":[{}],\"scope\":{},\"transport\":{},\"endpoint\":{}}}",
            json(&r.provider),
            json(&r.capability),
            json(&r.contract),
            r.guarantees.iter().map(|g| json(g)).collect::<Vec<_>>().join(","),
            json(r.scope.as_str()),
            json(&r.transport),
            json(&r.endpoint),
        ),
        None => String::new(),
    };
    format!(
        "{{\"version\":1,\"service\":\"abstraction.facade/resolver@1\",\"method\":\"Resolve\",\"ok\":true,\"payload\":{{\"value\":{{\"status\":{}{}}}}}}}",
        json(status),
        reference
    )
    .into_bytes()
}
/// CONTRACT.md FAC-B4: a resolved binding carries the reference the runtime
/// returned, so an application can name the provider that served it.
#[test]
fn a_resolved_binding_carries_the_reference_the_runtime_returned() {
    let served = reference();
    let c = connector(Some(served.clone()), "resolved");
    let binding = Machine::with_connector("resolver", c)
        .resolve_service(
            &served.contract,
            vec!["required".into()],
            wire::Scope::Remote,
        )
        .unwrap();
    assert_eq!(binding.reference(), Some(&served));
    assert_eq!(binding.reference().unwrap().provider, "implementation");
}
#[test]
fn alternate_connector_preserves_one_budget_and_fixed_binding() {
    let c = connector(Some(reference()), "resolved");
    let d = Instant::now() + Duration::from_secs(1);
    let m = Machine::with_connector("resolver", c.clone())
        .with_deadline(d)
        .with_cancellation(7);
    let b = m
        .resolve_service(
            "abstraction.job/acceptance@1",
            vec!["required".into()],
            wire::Scope::Remote,
        )
        .unwrap();
    assert_eq!(b.endpoint(), "selected");
    assert_eq!(b.reference().unwrap().provider, "implementation");
    assert_eq!(
        *c.calls.borrow(),
        vec![
            ("resolver".into(), d, Some(7), 1048576),
            ("selected".into(), d, Some(7), 1048576)
        ]
    );
    let b = b.with_limit(2097152).unwrap();
    assert_eq!(c.calls.borrow().last().unwrap().1, d);
    let later = d + Duration::from_secs(2);
    let fresh = b.with_waiting(Some(later), Some(9)).unwrap();
    assert_eq!(fresh.endpoint(), "selected");
    assert_eq!(
        fresh.reference().unwrap().contract,
        b.reference().unwrap().contract
    );
    assert_eq!(
        c.calls.borrow().last().unwrap(),
        &("selected".into(), later, Some(9), 2097152)
    );
    let restored = Binding::restore(c.clone(), fresh.endpoint(), Some(d), None, 2097152).unwrap();
    assert!(restored.reference().is_none());
    assert_eq!(restored.endpoint(), "selected");
}
#[test]
fn malformed_reference_never_connects_selected_endpoint() {
    let mutations: Vec<fn(&mut wire::ServiceReference)> = vec![
        |r| r.provider.clear(),
        |r| r.contract = "wrong".into(),
        |r| r.capability = "wrong".into(),
        |r| r.endpoint.clear(),
        |r| r.endpoint = "bad\0endpoint".into(),
        |r| r.guarantees.clear(),
        |r| r.guarantees.push("required".into()),
        |r| r.scope = wire::Scope::Local,
        |r| r.transport = "unknown".into(),
    ];
    for mutate in mutations {
        let mut r = reference();
        mutate(&mut r);
        let c = connector(Some(r), "resolved");
        assert!(Machine::with_connector("resolver", c.clone())
            .resolve_service(
                "abstraction.job/acceptance@1",
                vec!["required".into()],
                wire::Scope::Remote
            )
            .is_err());
        assert_eq!(c.calls.borrow().len(), 1);
    }
    for (r, status) in [(Some(reference()), "unavailable"), (None, "resolved")] {
        let c = connector(r, status);
        assert!(Machine::with_connector("resolver", c.clone())
            .resolve_service("abstraction.job/acceptance@1", vec![], wire::Scope::Remote)
            .is_err());
        assert_eq!(c.calls.borrow().len(), 1);
    }
}
#[test]
fn invalid_requirements_do_not_connect() {
    let c = connector(None, "unavailable");
    let m = Machine::with_connector("resolver", c.clone());
    assert!(matches!(
        m.resolve_service("job", vec!["".into()], wire::Scope::Local),
        Err(Error::InvalidRequirements)
    ));
    assert!(matches!(
        m.resolve_service("", vec![], wire::Scope::Any),
        Err(Error::InvalidRequirements)
    ));
    assert!(c.calls.borrow().is_empty());
}

#[test]
fn reusable_defaults_and_scoped_composite_waiting() {
    let c = connector(Some(reference()), "resolved");
    let b = Machine::with_connector("resolver", c.clone())
        .with_cancellation(7)
        .resolve_service("abstraction.job/acceptance@1", vec![], wire::Scope::Remote)
        .unwrap();
    let created = c.calls.borrow().last().unwrap().1;
    b.exchange_frame(b"first").unwrap();
    let first = c.calls.borrow().last().unwrap().1;
    assert!(first > created, "default call must start a fresh budget");
    b.exchange_frame(b"second").unwrap();
    assert!(c.calls.borrow().last().unwrap().1 > first);
    let scoped = b.call_scope().unwrap();
    let budget = c.calls.borrow().last().unwrap().clone();
    assert_eq!(budget.2, Some(7));
    assert_eq!(scoped.reference().unwrap().provider, "implementation");
    let count = c.calls.borrow().len();
    scoped.exchange_frame(b"chunk1").unwrap();
    scoped.clone().exchange_frame(b"chunk2").unwrap();
    assert_eq!(
        c.calls.borrow().len(),
        count,
        "scoped calls cannot renew waiting"
    );
    let explicit = Instant::now() - Duration::from_secs(1);
    let expired = b.with_waiting(Some(explicit), Some(9)).unwrap();
    expired
        .call_scope()
        .unwrap()
        .exchange_frame(b"expired")
        .unwrap();
    assert_eq!(c.calls.borrow().last().unwrap().1, explicit);
    assert_eq!(c.calls.borrow().last().unwrap().2, Some(9));
}

/// A transport failure with a message, so `source()` has something to show.
#[derive(Debug, PartialEq)]
struct Down(&'static str);
impl std::fmt::Display for Down {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.0)
    }
}
impl std::error::Error for Down {}

/// A connector whose installed runtime, listener and cancellation are chosen by the test.
#[derive(Clone)]
struct Scripted {
    installed: Option<&'static str>,
    listening: bool,
    cancelled: bool,
    reply: Rc<Vec<u8>>,
    /// A supported operating system unless a case names another.
    platform: &'static str,
}
impl Scripted {
    fn answering(reply: Vec<u8>) -> Self {
        Scripted {
            installed: Some("installed-pipe"),
            listening: true,
            cancelled: false,
            reply: Rc::new(reply),
            platform: "linux",
        }
    }
}
impl FrameTransport for Scripted {
    type Error = Down;
    fn write_frame(&self, _: &[u8]) -> Result<(), Down> {
        Ok(())
    }
    fn exchange_frame(&self, _: &[u8]) -> Result<Vec<u8>, Down> {
        if self.cancelled {
            return Err(Down("cancelled"));
        }
        if !self.listening {
            return Err(Down("nobody listens"));
        }
        Ok((*self.reply).clone())
    }
}
impl Connector for Scripted {
    type Transport = Self;
    type Cancellation = ();
    fn supports(&self, s: wire::Scope, t: &str) -> bool {
        s == wire::Scope::Local && t == "oa-framed-local@1"
    }
    fn connect(&self, _: &str, _: Instant, _: Option<()>, _: usize) -> Result<Self, Down> {
        Ok(self.clone())
    }
    fn runtime_endpoint(&self) -> Result<String, Down> {
        self.installed
            .map(String::from)
            .ok_or(Down("no runtime installed"))
    }
    fn is_cancellation(&self, error: &Down) -> bool {
        error.0 == "cancelled"
    }
    fn platform(&self) -> &str {
        self.platform
    }
}
fn resolution_error(
    result: Result<Binding<Scripted>, Error<Down>>,
) -> (ResolutionError<Down>, String, Option<String>) {
    let error = match result {
        Err(error) => error,
        Ok(_) => panic!("resolution produced a service"),
    };
    let message = error.to_string();
    let source = std::error::Error::source(&error).map(|e| e.to_string());
    match error {
        Error::Resolution(r) => (r, message, source),
        other => panic!("want Error::Resolution, got {other:?}"),
    }
}
const SINK: &str = "abstraction.logging/sink@1";

#[test]
fn unsupported_platform_names_itself_before_selection() {
    assert_eq!(unsupported_platform("android"), Some("android"));
    assert_eq!(unsupported_platform("macos"), None);
    assert_eq!(unsupported_platform("linux"), None);
    assert_eq!(unsupported_platform("windows"), None);
    let android = Scripted {
        installed: None,
        platform: "android",
        ..Scripted::answering(vec![])
    };
    let (r, message, source) = resolution_error(
        Machine::installed_with_connector(android.clone()).resolve_service(SINK, vec![], wire::Scope::Any),
    );
    assert_eq!(r.failure, ResolutionFailure::RuntimeUnavailable);
    assert_eq!(r.failure.status(), RUNTIME_UNAVAILABLE);
    assert_eq!(r.platform, Some("android"));
    assert_eq!(r.looked_for, "the installed runtime");
    assert_eq!((r.cause, source), (None, None));
    assert_eq!(message, "service resolution: runtime_unavailable: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime: no supported OpenAbstractions runtime exists for android");
    // An explicit endpoint is the application's own choice; platform does not refuse it.
    let unreachable = Scripted {
        listening: false,
        ..android
    };
    let (explicit, _, _) = resolution_error(
        Machine::with_connector("nobody", unreachable).resolve_service(SINK, vec![], wire::Scope::Any),
    );
    assert_eq!(explicit.platform, None);
}

#[test]
fn no_runtime_installed_is_a_resolution_error() {
    let absent = Scripted {
        installed: None,
        ..Scripted::answering(vec![])
    };
    let (r, message, source) = resolution_error(
        Machine::installed_with_connector(absent).resolve_service(SINK, vec![], wire::Scope::Any),
    );
    assert_eq!(r.failure, ResolutionFailure::RuntimeUnavailable);
    assert_eq!(
        (
            r.capability.as_str(),
            r.contract.as_str(),
            r.looked_for.as_str()
        ),
        ("abstraction.logging", SINK, "the installed runtime")
    );
    assert_eq!(r.cause, Some(Down("no runtime installed")));
    assert_eq!(source.as_deref(), Some("no runtime installed"));
    assert_eq!(message, "service resolution: runtime_unavailable: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime");
}

#[test]
fn explicit_endpoint_nobody_listens_on_is_a_resolution_error() {
    let silent = Scripted {
        listening: false,
        ..Scripted::answering(vec![])
    };
    let (r, _, source) = resolution_error(
        Machine::with_connector("absent-pipe", silent.clone()).resolve_service(SINK, vec![], wire::Scope::Any),
    );
    assert_eq!(r.failure, ResolutionFailure::RuntimeUnavailable);
    assert_eq!(r.looked_for, "the explicit endpoint absent-pipe");
    assert_eq!(source.as_deref(), Some("nobody listens"));
    let (r, _, _) = resolution_error(Machine::installed_with_connector(silent).resolve_service(
        SINK,
        vec![],
        wire::Scope::Any,
    ));
    assert_eq!(
        (r.failure.status(), r.looked_for.as_str()),
        (
            RUNTIME_UNAVAILABLE,
            "the installed runtime at installed-pipe"
        )
    );
}

#[test]
fn runtime_without_the_service_is_the_resolvers_refusal() {
    let empty = Scripted::answering(reply_frame(None, "unavailable"));
    let (r, _, source) = resolution_error(
        Machine::with_connector("resolver", empty).resolve_service(SINK, vec![], wire::Scope::Any),
    );
    assert_eq!(
        r.failure,
        ResolutionFailure::Refused(wire::ResolutionStatus::Unavailable)
    );
    assert_eq!(r.failure.status(), "unavailable");
    assert!(r.cause.is_none() && source.is_none());
    let mut local = reference();
    local.capability = "abstraction.logging".into();
    local.contract = SINK.into();
    local.guarantees.clear();
    local.transport = "https".into();
    let (r, message, _) = resolution_error(
        Machine::with_connector(
            "resolver",
            Scripted::answering(reply_frame(Some(local.clone()), "resolved")),
        )
        .resolve_service(SINK, vec![], wire::Scope::Any),
    );
    assert_eq!(
        r.failure,
        ResolutionFailure::UnsupportedTransport {
            scope: wire::Scope::Remote,
            transport: "https".into()
        }
    );
    assert!(
        message.ends_with(": remote reference over https"),
        "{message}"
    );
}

#[test]
fn success_is_unchanged() {
    let mut local = reference();
    local.capability = "abstraction.logging".into();
    local.contract = SINK.into();
    local.guarantees.clear();
    local.scope = wire::Scope::Local;
    local.transport = "oa-framed-local@1".into();
    let binding = Machine::installed_with_connector(Scripted::answering(reply_frame(
        Some(local),
        "resolved",
    )))
    .resolve_service(SINK, vec![], wire::Scope::Any)
    .unwrap();
    assert_eq!(binding.endpoint(), "selected");
}

#[test]
fn caller_cancellation_stays_a_transport_outcome() {
    let cancelled = Scripted {
        cancelled: true,
        ..Scripted::answering(vec![])
    };
    match Machine::with_connector("resolver", cancelled).resolve_service(SINK, vec![], wire::Scope::Any) {
        Err(Error::Call(wire::CallError::Transport(Down("cancelled")))) => {}
        Err(other) => panic!("cancellation became {other:?}"),
        Ok(_) => panic!("cancelled resolution produced a service"),
    }
}

/// A connector that records installed selection and the identity each step used.
#[derive(Clone)]
struct Selecting {
    identity: Option<&'static str>,
    refusal: Option<&'static str>,
    log: Rc<RefCell<Vec<String>>>,
    reply: Rc<Vec<u8>>,
}
impl Selecting {
    fn new(refusal: Option<&'static str>) -> Self {
        let mut local = reference();
        local.capability = "abstraction.logging".into();
        local.contract = SINK.into();
        local.guarantees.clear();
        local.scope = wire::Scope::Local;
        local.transport = "oa-framed-local@1".into();
        Selecting {
            identity: None,
            refusal,
            log: Rc::new(RefCell::new(vec![])),
            reply: Rc::new(reply_frame(Some(local), "resolved")),
        }
    }
    fn log(&self) -> Vec<String> {
        self.log.borrow().clone()
    }
}
impl FrameTransport for Selecting {
    type Error = Down;
    fn write_frame(&self, _: &[u8]) -> Result<(), Down> {
        Ok(())
    }
    fn exchange_frame(&self, _: &[u8]) -> Result<Vec<u8>, Down> {
        Ok((*self.reply).clone())
    }
}
impl Connector for Selecting {
    type Transport = Self;
    type Cancellation = ();
    fn supports(&self, s: wire::Scope, t: &str) -> bool {
        s == wire::Scope::Local && t == "oa-framed-local@1"
    }
    fn connect(&self, e: &str, _: Instant, _: Option<()>, _: usize) -> Result<Self, Down> {
        let as_whom = self.identity.unwrap_or("unverified");
        self.log
            .borrow_mut()
            .push(format!("connect {e} as {as_whom}"));
        Ok(self.clone())
    }
    fn runtime_endpoint(&self) -> Result<String, Down> {
        let as_whom = self.identity.unwrap_or("unverified");
        self.log.borrow_mut().push(format!("endpoint as {as_whom}"));
        Ok("installed-pipe".into())
    }
    fn select_installed(&self, _: Instant, _: Option<()>) -> Result<Self, Down> {
        self.log.borrow_mut().push("select".into());
        match self.refusal {
            Some(word) => Err(Down(word)),
            None => Ok(Selecting {
                identity: Some("installed-identity"),
                ..self.clone()
            }),
        }
    }
    fn is_cancellation(&self, error: &Down) -> bool {
        error.0 == "cancelled"
    }
}

#[test]
fn installed_resolution_verifies_the_selected_identity_on_every_connection() {
    let c = Selecting::new(None);
    let binding = Machine::installed_with_connector(c.clone())
        .resolve_service(SINK, vec![], wire::Scope::Any)
        .unwrap();
    binding.exchange_frame(b"call").unwrap();
    binding.call_scope().unwrap();
    assert_eq!(
        c.log(),
        [
            "select",
            "endpoint as installed-identity",
            "connect installed-pipe as installed-identity",
            "connect selected as installed-identity",
            "connect selected as installed-identity",
            "connect selected as installed-identity",
        ]
    );
    // An explicit endpoint is the application's choice and selects nothing.
    let explicit = Selecting::new(None);
    Machine::with_connector("resolver", explicit.clone())
        .resolve_service(SINK, vec![], wire::Scope::Any)
        .unwrap();
    assert_eq!(
        explicit.log(),
        [
            "connect resolver as unverified",
            "connect selected as unverified"
        ]
    );
}

#[test]
fn selection_refusal_contacts_nothing() {
    let c = Selecting::new(Some("no installation"));
    let (r, message, source) =
        match Machine::installed_with_connector(c.clone()).resolve_service(SINK, vec![], wire::Scope::Any) {
            Err(error) => {
                let message = error.to_string();
                let source = std::error::Error::source(&error).map(|e| e.to_string());
                match error {
                    Error::Resolution(r) => (r, message, source),
                    other => panic!("want Error::Resolution, got {other:?}"),
                }
            }
            Ok(_) => panic!("refused selection produced a service"),
        };
    assert_eq!(r.failure, ResolutionFailure::RuntimeUnavailable);
    assert_eq!(r.looked_for, "the installed runtime");
    assert_eq!(source.as_deref(), Some("no installation"));
    assert!(message.ends_with("at the installed runtime"), "{message}");
    assert_eq!(c.log(), ["select"]);
    let cancelled = Selecting::new(Some("cancelled"));
    match Machine::installed_with_connector(cancelled.clone()).resolve_service(SINK, vec![], wire::Scope::Any)
    {
        Err(Error::Transport(Down("cancelled"))) => {}
        Err(other) => panic!("cancellation became {other:?}"),
        Ok(_) => panic!("cancelled selection produced a service"),
    }
    assert_eq!(cancelled.log(), ["select"]);
}
#[test]
fn describe_endpoint_reads_the_description_through_the_connector() {
    let reply = "{\"version\":1,\"service\":\"abstraction.facade/endpoint@1\",\"method\":\"Describe\",\"ok\":true,\"payload\":{\"value\":{\"outcome\":\"described\",\"program\":\"fixture\",\"version\":\"1\",\"services\":[{\"contract\":\"abstraction.logging/sink@1\",\"readiness\":\"not_ready\",\"why\":\"journal:unreadable\",\"guarantees\":[],\"capabilities\":{}}]}}}";
    let c = Alternate {
        calls: Rc::new(RefCell::new(vec![])),
        reply: Rc::new(reply.as_bytes().to_vec()),
    };
    let m = Machine::with_connector("resolver", c.clone());
    let description = m.describe_endpoint("provider-endpoint").unwrap();
    assert_eq!(description.program, "fixture");
    assert_eq!(description.services.len(), 1);
    assert_eq!(description.services[0].contract, "abstraction.logging/sink@1");
    assert_eq!(description.services[0].why, "journal:unreadable");
    assert_eq!(c.calls.borrow()[0].0, "provider-endpoint");
}
#[test]
fn resolve_registry_binds_the_resolved_registry_endpoint() {
    let mut r = reference();
    r.capability = "abstraction.facade".into();
    r.contract = "abstraction.facade/registry@1".into();
    r.guarantees = vec![];
    let c = connector(Some(r), "resolved");
    let registry = Machine::with_connector("resolver", c.clone())
        .resolve_registry(vec![], wire::Scope::Remote)
        .unwrap();
    assert_eq!(registry.transport().endpoint(), "selected");
}
