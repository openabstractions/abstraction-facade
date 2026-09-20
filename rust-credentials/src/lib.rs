//! Resolved credentials holder and designated-enforcer applier.
//!
//! The holder registers, rotates, revokes, lists and audits credentials by name
//! in the caller's account; no call returns secret bytes. The applier serves
//! only programs the receiving host designated as enforcers. No call is retried.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_credentials_api as wire;
use wire::{Applier, Holder};

#[derive(Debug)]
pub enum Error<E> {
    Call(wire::CallError<E>),
    Invalid(&'static str),
}
impl<E: std::fmt::Debug> std::fmt::Display for Error<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl<E: std::fmt::Debug> std::error::Error for Error<E> {}

fn require<E>(ok: bool, s: &'static str) -> Result<(), Error<E>> {
    if ok {
        Ok(())
    } else {
        Err(Error::Invalid(s))
    }
}

/// A credential name is 1..64 bytes of A-Z a-z 0-9 _ - .
pub fn name(s: &str) -> bool {
    (1..=64).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'-' | b'.'))
}

/// A stored or conflicting reply carries metadata; other outcomes carry none.
pub fn check_store<E>(r: &wire::StoreResult) -> Result<(), Error<E>> {
    let evaluated = r.outcome == "stored" || r.outcome == "conflict";
    require(evaluated == r.current.is_some() && (r.outcome != "stored" || !r.revision.is_empty()), "store result")
}

/// A page holds at most limit records and a nonempty next exactly when incomplete;
/// a refusal holds nothing.
pub fn check_page<E>(r: &wire::MetadataPage, limit: i64) -> Result<(), Error<E>> {
    if r.outcome == "page" {
        require(r.records.len() as i64 <= limit && r.complete == r.next.is_empty(), "metadata page")
    } else {
        require(r.records.is_empty() && r.next.is_empty() && !r.complete, "metadata page refusal")
    }
}

/// Headers are present exactly for applied.
pub fn check_apply<E>(r: &wire::ApplyResult) -> Result<(), Error<E>> {
    require((r.outcome == "applied") == !r.headers.is_empty(), "apply headers")
}

/// Registers, rotates, revokes and inspects credentials in the caller's account.
#[derive(Clone)]
pub struct Credentials<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Credentials<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    /// The caller clears the registration's secret after the call.
    pub fn store(&self, expected_revision: &str, registration: wire::Registration) -> Result<wire::StoreResult, Error<T::Error>> {
        require(name(&registration.name) && !registration.secret.is_empty(), "registration")?;
        let r = wire::HolderClient::new(self.transport.clone())
            .store(expected_revision.into(), registration)
            .map_err(Error::Call)?;
        check_store(&r)?;
        Ok(r)
    }
    pub fn rotate(&self, expected_revision: &str, rotation: wire::Rotation) -> Result<wire::RotateResult, Error<T::Error>> {
        require(name(&rotation.name) && !rotation.secret.is_empty() && !expected_revision.is_empty(), "rotation")?;
        let r = wire::HolderClient::new(self.transport.clone())
            .rotate(expected_revision.into(), rotation)
            .map_err(Error::Call)?;
        require(r.outcome != "rotated" || !r.revision.is_empty(), "rotate result")?;
        Ok(r)
    }
    pub fn revoke(&self, expected_revision: &str, credential: &str) -> Result<wire::RevokeResult, Error<T::Error>> {
        require(name(credential) && !expected_revision.is_empty(), "revocation")?;
        wire::HolderClient::new(self.transport.clone())
            .revoke(expected_revision.into(), credential.into())
            .map_err(Error::Call)
    }
    pub fn list(&self, cursor: &str, limit: i64) -> Result<wire::MetadataPage, Error<T::Error>> {
        require(cursor.len() <= 256 && (1..=64).contains(&limit), "list range")?;
        let r = wire::HolderClient::new(self.transport.clone())
            .list(cursor.into(), limit)
            .map_err(Error::Call)?;
        check_page(&r, limit)?;
        Ok(r)
    }
    pub fn audit(&self, cursor: &str, max_entries: i64) -> Result<wire::AuditPage, Error<T::Error>> {
        require(cursor.len() <= 256 && (1..=256).contains(&max_entries), "audit range")?;
        let r = wire::HolderClient::new(self.transport.clone())
            .audit(cursor.into(), max_entries)
            .map_err(Error::Call)?;
        require(r.entries.len() as i64 <= max_entries, "audit page")?;
        Ok(r)
    }
}

/// Applies a named credential to one request for a designated consuming service.
#[derive(Clone)]
pub struct CredentialApplier<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> CredentialApplier<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn check(&self, usage: wire::Use) -> Result<wire::CheckResult, Error<T::Error>> {
        require(name(&usage.name), "use")?;
        wire::ApplierClient::new(self.transport.clone()).check(usage).map_err(Error::Call)
    }
    /// Send the headers once and keep them out of every record, log and reply.
    pub fn apply(&self, usage: wire::Use) -> Result<wire::ApplyResult, Error<T::Error>> {
        require(name(&usage.name), "use")?;
        let r = wire::ApplierClient::new(self.transport.clone()).apply(usage).map_err(Error::Call)?;
        check_apply(&r)?;
        Ok(r)
    }
}

pub trait CredentialsMachine<C: Connector> {
    /// Resolution grants nothing; every holder call is a rights decision.
    fn resolve_credentials(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Credentials<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
    /// Only a program the host designated as an enforcer receives anything but forbidden.
    fn resolve_credentials_applier(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<CredentialApplier<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> CredentialsMachine<C> for Machine<C> {
    fn resolve_credentials(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Credentials<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Credentials::new(self.resolve_service("abstraction.credentials/holder@1", g, s)?))
    }
    fn resolve_credentials_applier(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<CredentialApplier<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(CredentialApplier::new(self.resolve_service("abstraction.credentials/applier@1", g, s)?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;
    use std::rc::Rc;

    /// Answers every exchange with one fixed reply and keeps the request frames.
    #[derive(Clone)]
    struct Fixed {
        reply: Vec<u8>,
        sent: Rc<RefCell<Vec<Vec<u8>>>>,
    }
    impl wire::FrameTransport for Fixed {
        type Error = ();
        fn write_frame(&self, _: &[u8]) -> Result<(), ()> {
            Err(())
        }
        fn exchange_frame(&self, frame: &[u8]) -> Result<Vec<u8>, ()> {
            self.sent.borrow_mut().push(frame.to_vec());
            Ok(self.reply.clone())
        }
    }

    #[test]
    fn results_keep_their_shapes() {
        assert!(name("hf") && name("ollama-cloud") && !name("") && !name("bad name") && !name(&"a".repeat(65)));
        let mut page = wire::MetadataPage {
            outcome: wire::PageOutcome::Forbidden,
            limits: wire::Limits::default(),
            records: vec![],
            next: String::new(),
            complete: false,
        };
        assert!(check_page::<()>(&page, 8).is_ok());
        page.complete = true;
        assert!(check_page::<()>(&page, 8).is_err());
        page.outcome = wire::PageOutcome::Page;
        assert!(check_page::<()>(&page, 8).is_ok());
        let mut applied =
            wire::ApplyResult { outcome: wire::ApplyOutcome::Forbidden, revision: String::new(), headers: Default::default() };
        assert!(check_apply::<()>(&applied).is_ok());
        applied.headers.insert("Authorization".into(), "Bearer x".into());
        assert!(check_apply::<()>(&applied).is_err());
        applied.outcome = wire::ApplyOutcome::Applied;
        assert!(check_apply::<()>(&applied).is_ok());
        let stored = wire::StoreResult { outcome: wire::StoreOutcome::Stored, revision: "1-ab".into(), current: None };
        assert!(check_store::<()>(&stored).is_err());
    }

    #[test]
    fn list_exchanges_one_holder_frame() {
        let reply = br#"{"version":1,"service":"abstraction.credentials/holder@1","method":"List","ok":true,"payload":{"value":{"outcome":"forbidden","limits":{"max_credentials":0,"max_secret_bytes":0,"tombstone_retention_ms":0,"audit_retention_ms":0,"audit_capacity":0,"secure_store":"","supported_kinds":[]},"records":[],"next":"","complete":false}}}"#;
        let sent = Rc::new(RefCell::new(Vec::new()));
        let holder = Credentials::new(Fixed { reply: reply.to_vec(), sent: sent.clone() });
        let page = holder.list("", 8).expect("list");
        assert_eq!(page.outcome, wire::PageOutcome::Forbidden);
        let frame: String = String::from_utf8(sent.borrow()[0].clone()).unwrap().chars().filter(|c| !c.is_whitespace()).collect();
        assert!(frame.contains("\"service\":\"abstraction.credentials/holder@1\"") && frame.contains("\"method\":\"List\""), "{frame}");
        assert!(holder.list("", 0).is_err());
        assert_eq!(sent.borrow().len(), 1, "an invalid range sends nothing");
    }
}
