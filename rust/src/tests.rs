use super::*;
use std::{cell::RefCell, rc::Rc};
#[derive(Clone)]
struct Alternate {
    calls: Rc<RefCell<Vec<(String, Instant, Option<u8>, usize)>>>,
    reply: Rc<Vec<u8>>,
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
    fn supports(&self, s: &str, t: &str) -> bool {
        s == "remote" && t == "test-framed@1"
    }
    fn connect(&self, e: &str, d: Instant, c: Option<u8>, n: usize) -> Result<Self, ()> {
        self.calls.borrow_mut().push((e.into(), d, c, n));
        Ok(self.clone())
    }
}
fn reference() -> wire::ServiceReference {
    wire::ServiceReference {
        capability: "abstraction.job".into(),
        contract: "abstraction.job/acceptance@1".into(),
        provider: "implementation".into(),
        endpoint: "selected".into(),
        scope: "remote".into(),
        transport: "test-framed@1".into(),
        guarantees: vec!["required".into()],
    }
}
fn connector(r: Option<wire::ServiceReference>, status: &str) -> Alternate {
    let mut payload = vec![];
    wire::enc_oaresolverresolveresult(
        &mut payload,
        &wire::OAResolverResolveResult {
            value: wire::ResolveResult {
                status: status.into(),
                reference: r,
            },
        },
        1,
    );
    let mut reply = vec![];
    wire::enc_oaservicereply(
        &mut reply,
        &wire::OAServiceReply {
            version: 1,
            service: "abstraction.facade/resolver@1".into(),
            method: "Resolve".into(),
            ok: true,
            payload,
        },
        0,
    );
    Alternate {
        calls: Rc::new(RefCell::new(vec![])),
        reply: Rc::new(reply),
    }
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
            "remote",
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
        |r| r.scope = "local".into(),
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
                "remote"
            )
            .is_err());
        assert_eq!(c.calls.borrow().len(), 1);
    }
    for (r, status) in [(Some(reference()), "unavailable"), (None, "resolved")] {
        let c = connector(r, status);
        assert!(Machine::with_connector("resolver", c.clone())
            .resolve_service("abstraction.job/acceptance@1", vec![], "remote")
            .is_err());
        assert_eq!(c.calls.borrow().len(), 1);
    }
}
#[test]
fn invalid_requirements_do_not_connect() {
    let c = connector(None, "unavailable");
    let m = Machine::with_connector("resolver", c.clone());
    assert!(matches!(
        m.resolve_service("job", vec!["".into()], "local"),
        Err(Error::InvalidRequirements)
    ));
    assert!(matches!(
        m.resolve_service("job", vec![], "invalid"),
        Err(Error::InvalidRequirements)
    ));
    assert!(c.calls.borrow().is_empty());
}

#[test]
fn reusable_defaults_and_scoped_composite_waiting() {
    let c = connector(Some(reference()), "resolved");
    let b = Machine::with_connector("resolver", c.clone())
        .with_cancellation(7)
        .resolve_service("abstraction.job/acceptance@1", vec![], "remote")
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
