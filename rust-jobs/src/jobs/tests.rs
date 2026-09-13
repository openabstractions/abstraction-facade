use super::*;
use std::{cell::RefCell, collections::VecDeque, rc::Rc};
#[derive(Clone)]
struct Replies(Rc<RefCell<VecDeque<Vec<u8>>>>);
impl wire::FrameTransport for Replies {
    type Error = ();
    fn write_frame(&self, _: &[u8]) -> Result<(), ()> {
        panic!("unexpected one-way")
    }
    fn exchange_frame(&self, _: &[u8]) -> Result<Vec<u8>, ()> {
        Ok(self
            .0
            .borrow_mut()
            .pop_front()
            .expect("unexpected extra call"))
    }
}
fn wanted() -> wire::RequestIdentity {
    wire::RequestIdentity {
        key: "key".into(),
        history_epoch: "epoch".into(),
    }
}
fn receipt() -> wire::Receipt {
    wire::Receipt {
        identity: wanted(),
        logical_owner: "owner".into(),
        operation_id: "operation".into(),
        accepted_guarantees: vec!["required".into()],
        history_retention_ms: 1,
    }
}
fn client(replies: Vec<Vec<u8>>) -> Jobs<Replies> {
    Jobs::new(
        Replies(Rc::new(RefCell::new(replies.into()))),
        "owner".into(),
        vec!["required".into()],
    )
    .unwrap()
}
#[test]
fn receipt_owner_identity_guarantees_and_combinations() {
    for change in [0, 1, 2, 3, 4, 5] {
        let c = client(vec![]);
        let mut r = receipt();
        match change {
            0 => r.logical_owner = "forged".into(),
            1 => r.identity.key = "other".into(),
            2 => r.accepted_guarantees.clear(),
            3 => r.accepted_guarantees.push("required".into()),
            4 => r.operation_id.clear(),
            _ => r.history_retention_ms = 0,
        };
        assert!(c
            .acceptance(
                wire::AcceptanceResult {
                    outcome: "accepted".into(),
                    receipt: Some(r),
                    ..Default::default()
                },
                &wanted(),
                &c.required
            )
            .is_err());
        assert_eq!(c.owner(), "owner");
    }
    let c = client(vec![]);
    assert!(c
        .acceptance(
            wire::AcceptanceResult {
                outcome: "unknown".into(),
                receipt: Some(receipt()),
                ..Default::default()
            },
            &wanted(),
            &[]
        )
        .is_err());
    assert_eq!(
        c.acceptance(
            wire::AcceptanceResult {
                outcome: "unknown".into(),
                ..Default::default()
            },
            &wanted(),
            &[]
        )
        .unwrap()
        .outcome,
        "unknown"
    );
}
// Hostile wire inputs exercise the generated decoder followed by the typed validator.
fn chunk(offset: i64, total: i64, data: &str, eof: bool, operation: &str) -> Vec<u8> {
    format!(r#"{{"version":1,"service":"abstraction.job/operations@1","method":"ReadResult","ok":true,"payload":{{"value":{{"outcome":"data","chunk":{{"receipt":{{"identity":{{"key":"key","history_epoch":"epoch"}},"logical_owner":"owner","operation_id":"{operation}","accepted_guarantees":["required"],"history_retention_ms":1}},"offset":{offset},"total":{total},"data":"{data}","eof":{eof}}}}}}}}}"#).into_bytes()
}
#[test]
fn copy_bounds_immutability_and_empty() {
    let mut out = Vec::new();
    assert_eq!(
        client(vec![
            chunk(0, 2, "YQ==", false, "operation"),
            chunk(1, 2, "Yg==", true, "operation")
        ])
        .copy_result(&wanted(), &mut out)
        .unwrap(),
        2
    );
    assert_eq!(out, b"ab");
    assert_eq!(
        client(vec![chunk(0, 0, "", true, "operation")])
            .copy_result(&wanted(), &mut Vec::new())
            .unwrap(),
        0
    );
    for second in [
        chunk(1, 3, "Yg==", false, "operation"),
        chunk(1, 2, "Yg==", true, "changed"),
    ] {
        let e = client(vec![chunk(0, 2, "YQ==", false, "operation"), second])
            .copy_result(&wanted(), &mut Vec::new())
            .unwrap_err();
        assert_eq!(e.confirmed, 1);
        assert!(matches!(e.cause, Error::Invalid("result changed")));
    }
    for bad in [
        chunk(0, 1, "", false, "operation"),
        chunk(0, 2, "YQ==", true, "operation"),
        chunk(1, 2, "YQ==", true, "operation"),
    ] {
        assert!(client(vec![bad]).read_result(&wanted(), 0, 1).is_err());
    }
}
struct Fails {
    calls: u8,
}
impl Write for Fails {
    fn write(&mut self, b: &[u8]) -> std::io::Result<usize> {
        self.calls += 1;
        if self.calls == 1 {
            Ok(b.len().min(1))
        } else {
            Err(std::io::ErrorKind::BrokenPipe.into())
        }
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}
#[test]
fn writer_error_retains_confirmed_prefix() {
    let e = client(vec![chunk(0, 2, "YWI=", true, "operation")])
        .copy_result(&wanted(), &mut Fails { calls: 0 })
        .unwrap_err();
    assert_eq!(e.confirmed, 1);
    assert!(matches!(e.cause, Error::Writer(_)));
}
#[test]
fn negative_progress_refused_last_retry_preserved() {
    let c = client(vec![]);
    let mut s = wire::OperationSnapshot {
        receipt: receipt(),
        state: "pending".into(),
        progress: wire::WorkProgress { done: 2, total: 1 },
        failure: Some(wire::WorkFailure {
            classification: "retryable".into(),
            message: "last attempt".into(),
        }),
        ..Default::default()
    };
    assert!(c.snapshot(&s, &wanted(), &c.required).is_ok());
    s.progress.done = -1;
    assert!(c.snapshot(&s, &wanted(), &[]).is_err());
}

#[test]
fn nondata_copy_preserves_typed_outcome_without_retry() {
    let raw=br#"{"version":1,"service":"abstraction.job/operations@1","method":"ReadResult","ok":true,"payload":{"value":{"outcome":"not_ready"}}}"#.to_vec();
    let e = client(vec![raw])
        .copy_result(&wanted(), &mut Vec::new())
        .unwrap_err();
    assert_eq!(e.confirmed, 0);
    assert!(matches!(e.cause,Error::Outcome(s) if s=="not_ready"));
}

#[test]
fn inventory_continuation_and_gap() {
    let page = |outcome: &str, next: &str, complete: bool| {
        format!(r#"{{"version":1,"service":"abstraction.job/inventory@1","method":"ListWork","ok":true,"payload":{{"value":{{"outcome":"{outcome}","snapshots":[],"next":"{next}","complete":{complete}}}}}}}"#).into_bytes()
    };
    let inv = Inventory(client(vec![
        page("page", "cursor", false),
        page("gap", "", false),
        page("page", "cursor", false),
    ]));
    assert_eq!(inv.list("", 1).unwrap().next, "cursor");
    assert_eq!(inv.list("cursor", 1).unwrap().outcome, "gap");
    assert!(inv.list("cursor", 1).is_err());
}

#[test]
fn fresh_waiting_can_pin_later_but_restore_requires_owner() {
    let binding =
        super::super::Binding::restore(TestConnector, "unused", None, None, 2097152).unwrap();
    let fresh = Jobs::new(binding, String::new(), vec![]).unwrap();
    let waiting = fresh.with_waiting(None, None).unwrap();
    assert!(waiting.owner().is_empty());
    waiting.pin("durable-owner-B").unwrap();
    assert_eq!(fresh.owner(), "durable-owner-B");
    assert!(fresh.pin("implementation-A").is_err());
    assert!(Jobs::restore(TestConnector, "unused", String::new(), vec![], None, None).is_err());
    let restored = Jobs::restore(
        TestConnector,
        "unused",
        "persisted-owner".into(),
        vec![],
        None,
        None,
    )
    .unwrap();
    assert!(restored.pin("another-owner").is_err());
}

#[derive(Clone)]
struct TestConnector;
impl super::super::Connector for TestConnector {
    type Transport = Replies;
    type Cancellation = ();
    fn supports(&self, _: &str, _: &str) -> bool {
        true
    }
    fn connect(
        &self,
        _: &str,
        _: std::time::Instant,
        _: Option<()>,
        _: usize,
    ) -> Result<Replies, ()> {
        Ok(Replies(Rc::new(RefCell::new(VecDeque::new()))))
    }
}

impl abstraction_facade_service::ScopedTransport for Replies {
    type Scoped = Self;
    fn call_scope(&self) -> Result<Self, ()> {
        Ok(self.clone())
    }
}

#[derive(Clone)]
struct BudgetReplies {
    replies: Replies,
    remaining: Option<Rc<std::cell::Cell<u8>>>,
}
impl wire::FrameTransport for BudgetReplies {
    type Error = ();
    fn write_frame(&self, _: &[u8]) -> Result<(), ()> {
        unreachable!()
    }
    fn exchange_frame(&self, frame: &[u8]) -> Result<Vec<u8>, ()> {
        if let Some(remaining) = &self.remaining {
            if remaining.get() == 0 {
                return Err(());
            }
            remaining.set(remaining.get() - 1);
        }
        self.replies.exchange_frame(frame)
    }
}
impl abstraction_facade_service::ScopedTransport for BudgetReplies {
    type Scoped = Self;
    fn call_scope(&self) -> Result<Self, ()> {
        Ok(Self {
            replies: self.replies.clone(),
            remaining: Some(Rc::new(std::cell::Cell::new(1))),
        })
    }
}
#[test]
fn copy_uses_one_transport_scope_and_retains_confirmed_prefix() {
    let transport = BudgetReplies {
        replies: Replies(Rc::new(RefCell::new(
            vec![
                chunk(0, 2, "YQ==", false, "operation"),
                chunk(1, 2, "Yg==", true, "operation"),
            ]
            .into(),
        ))),
        remaining: None,
    };
    let client = Jobs::new(transport, "owner".into(), vec!["required".into()]).unwrap();
    let mut output = Vec::new();
    let error = client.copy_result(&wanted(), &mut output).unwrap_err();
    assert_eq!(error.confirmed, 1);
    assert_eq!(output, b"a");
    assert!(matches!(
        error.cause,
        Error::Call(wire::CallError::Transport(()))
    ));
}
