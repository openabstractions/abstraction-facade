//! Resolved rights decisions and conditional policy administration.
//!
//! Decisions are point-in-time observations: evaluated outcomes carry the
//! policy revision they observed and refusals carry none. Administration
//! compares an expected revision; a lost reply is uncertain and is never
//! retried. Resource services remain the receiving enforcement points.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_rights_api as wire;
use std::path::{Component, Path};
use wire::{Authorization, AuthorizationOperator};

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

fn text(s: &str, max: usize) -> bool {
    !s.is_empty() && s.len() <= max && !s.chars().any(char::is_control)
}

/// Account 1..128 bytes; program an absolute path without `.` or `..` components.
fn subject(s: &wire::Subject) -> bool {
    let path = Path::new(&s.program);
    text(&s.account, 128)
        && text(&s.program, 4096)
        && path.is_absolute()
        && !path.components().any(|c| matches!(c, Component::CurDir | Component::ParentDir))
}

fn rule(r: &wire::PolicyRule) -> bool {
    subject(&r.subject) && text(&r.action, 128) && text(&r.resource, 1024)
}

const EVALUATED: [&str; 4] = ["permitted", "denied", "not_granted", "unknown_action"];

/// Evaluated outcomes carry a revision; `invalid`, `forbidden` and `unavailable` carry none.
pub fn check_decision<E>(d: &wire::Decision) -> Result<(), Error<E>> {
    let evaluated = EVALUATED.contains(&d.outcome.as_str());
    require(evaluated || matches!(d.outcome.as_str(), "invalid" | "forbidden" | "unavailable"), "decision outcome")?;
    require(evaluated != d.policy_revision.is_empty(), "decision revision")
}

/// Refusals carry nothing; a page carries a revision, a sorted catalogue and
/// catalogued unique rules, and a nonempty cursor exactly when incomplete.
pub fn check_page<E>(p: &wire::PolicyPage, cursor: &str, limit: i64) -> Result<(), Error<E>> {
    if p.outcome != "page" {
        return require(
            p.revision.is_empty() && p.catalog.is_empty() && p.rules.is_empty() && p.next.is_empty() && !p.complete,
            "policy refusal",
        );
    }
    require(
        text(&p.revision, 128)
            && (1..=64).contains(&p.catalog.len())
            && p.rules.len() as i64 <= limit
            && p.next.len() <= 256
            && p.complete == p.next.is_empty()
            && (p.complete || (!p.rules.is_empty() && p.next != cursor)),
        "policy page",
    )?;
    require(
        p.catalog.iter().all(|a| text(a, 128)) && p.catalog.windows(2).all(|w| w[0] < w[1]),
        "policy catalogue",
    )?;
    let mut seen = std::collections::BTreeSet::new();
    for r in &p.rules {
        let key = (&r.subject.account, &r.subject.program, &r.action, &r.resource);
        require(rule(r) && p.catalog.contains(&r.action) && seen.insert(key), "policy rule")?;
    }
    Ok(())
}

/// `applied` and `conflict` carry a revision and an optional current rule for
/// the edited target; an applied set carries the set rule and an applied revoke none.
pub fn check_edit<E>(
    e: &wire::PolicyEdit,
    target: &wire::Subject,
    action: &str,
    resource: &str,
    permit: Option<bool>,
) -> Result<(), Error<E>> {
    let observed = matches!(e.outcome.as_str(), "applied" | "conflict");
    require(observed || matches!(e.outcome.as_str(), "invalid" | "forbidden" | "unavailable"), "edit outcome")?;
    require(observed == text(&e.revision, 128) && (observed || e.current.is_none()), "edit revision")?;
    if let Some(c) = &e.current {
        require(
            rule(c)
                && c.subject.account == target.account
                && c.subject.program == target.program
                && c.action == action
                && c.resource == resource,
            "current rule",
        )?;
    }
    if e.outcome == "applied" {
        require(e.current.as_ref().map(|c| c.permit) == permit, "applied state")?;
    }
    Ok(())
}

/// Decisions for the bound caller, or for a subject when the service trusts
/// this program as an enforcer.
#[derive(Clone)]
pub struct Decisions<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Decisions<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn decide(&self, action: &str, resource: &str) -> Result<wire::Decision, Error<T::Error>> {
        let d = wire::AuthorizationClient::new(self.transport.clone())
            .decide(action.into(), resource.into())
            .map_err(Error::Call)?;
        check_decision::<T::Error>(&d)?;
        Ok(d)
    }
    pub fn decide_for(
        &self,
        subject: wire::Subject,
        action: &str,
        resource: &str,
    ) -> Result<wire::Decision, Error<T::Error>> {
        let d = wire::AuthorizationClient::new(self.transport.clone())
            .decide_for(subject, action.into(), resource.into())
            .map_err(Error::Call)?;
        check_decision::<T::Error>(&d)?;
        Ok(d)
    }
}

/// Conditional policy administration at an expected revision.
#[derive(Clone)]
pub struct Operator<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Operator<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn list_policy(&self, cursor: &str, limit: i64) -> Result<wire::PolicyPage, Error<T::Error>> {
        require(cursor.len() <= 256 && (1..=64).contains(&limit), "policy range")?;
        let p = wire::AuthorizationOperatorClient::new(self.transport.clone())
            .list_policy(cursor.into(), limit)
            .map_err(Error::Call)?;
        check_page::<T::Error>(&p, cursor, limit)?;
        Ok(p)
    }
    pub fn set_rule(&self, expected_revision: &str, r: wire::PolicyRule) -> Result<wire::PolicyEdit, Error<T::Error>> {
        require(text(expected_revision, 128) && rule(&r), "policy edit")?;
        let target = wire::Subject { account: r.subject.account.clone(), program: r.subject.program.clone() };
        let (action, resource, permit) = (r.action.clone(), r.resource.clone(), r.permit);
        let e = wire::AuthorizationOperatorClient::new(self.transport.clone())
            .set_rule(expected_revision.into(), r)
            .map_err(Error::Call)?;
        check_edit::<T::Error>(&e, &target, &action, &resource, Some(permit))?;
        Ok(e)
    }
    pub fn revoke_rule(
        &self,
        expected_revision: &str,
        target: wire::Subject,
        action: &str,
        resource: &str,
    ) -> Result<wire::PolicyEdit, Error<T::Error>> {
        require(
            text(expected_revision, 128) && subject(&target) && text(action, 128) && text(resource, 1024),
            "policy revoke",
        )?;
        let copy = wire::Subject { account: target.account.clone(), program: target.program.clone() };
        let e = wire::AuthorizationOperatorClient::new(self.transport.clone())
            .revoke_rule(expected_revision.into(), target, action.into(), resource.into())
            .map_err(Error::Call)?;
        check_edit::<T::Error>(&e, &copy, action, resource, None)?;
        Ok(e)
    }
}

pub trait RightsMachine<C: Connector> {
    fn resolve_rights(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Decisions<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
    /// Every call remains subject to the host's operator authorization.
    fn resolve_rights_operator(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Operator<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> RightsMachine<C> for Machine<C> {
    fn resolve_rights(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Decisions<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Decisions::new(self.resolve_service("abstraction.rights/authorization@1", g, s)?))
    }
    fn resolve_rights_operator(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Operator<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Operator::new(self.resolve_service("abstraction.rights/operator@1", g, s)?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn target() -> wire::Subject {
        #[cfg(windows)]
        let program = "C:\\Program Files\\app.exe";
        #[cfg(not(windows))]
        let program = "/usr/bin/app";
        wire::Subject { account: "account".into(), program: program.into() }
    }
    fn policy_rule(permit: bool) -> wire::PolicyRule {
        wire::PolicyRule { subject: target(), action: "fixture.read".into(), resource: "r".into(), permit }
    }
    #[test]
    fn decisions_carry_revisions_only_when_evaluated() {
        use wire::DecisionOutcome::{Denied, Forbidden, Permitted, Unavailable};
        let d = |outcome, revision: &str| wire::Decision { outcome, policy_revision: revision.into() };
        assert!(check_decision::<()>(&d(Permitted, "r1")).is_ok());
        assert!(check_decision::<()>(&d(Denied, "")).is_err());
        assert!(check_decision::<()>(&d(Unavailable, "")).is_ok());
        assert!(check_decision::<()>(&d(Forbidden, "r1")).is_err());
    }
    #[test]
    fn pages_and_edits_keep_their_shapes() {
        let mut page = wire::PolicyPage {
            outcome: wire::PolicyPageOutcome::Page,
            revision: "r1".into(),
            catalog: vec!["fixture.read".into()],
            rules: vec![policy_rule(true)],
            next: String::new(),
            complete: true,
        };
        assert!(check_page::<()>(&page, "", 16).is_ok());
        page.rules.push(policy_rule(false));
        assert!(check_page::<()>(&page, "", 16).is_err(), "duplicate rule target");
        page.rules.pop();
        page.rules[0].action = "fixture.other".into();
        assert!(check_page::<()>(&page, "", 16).is_err(), "uncatalogued action");
        let refused = |outcome, revision: &str| wire::PolicyPage {
            outcome,
            revision: revision.into(),
            catalog: vec![],
            rules: vec![],
            next: String::new(),
            complete: false,
        };
        let refusal = refused(wire::PolicyPageOutcome::Forbidden, "");
        assert!(check_page::<()>(&refusal, "", 16).is_ok());
        let leaked = refused(wire::PolicyPageOutcome::Unavailable, "r1");
        assert!(check_page::<()>(&leaked, "", 16).is_err());

        let t = target();
        let applied = wire::PolicyEdit { outcome: wire::PolicyEditOutcome::Applied, revision: "r2".into(), current: Some(policy_rule(true)) };
        assert!(check_edit::<()>(&applied, &t, "fixture.read", "r", Some(true)).is_ok());
        assert!(check_edit::<()>(&applied, &t, "fixture.read", "r", Some(false)).is_err());
        assert!(check_edit::<()>(&applied, &t, "fixture.read", "other", Some(true)).is_err());
        let revoked = wire::PolicyEdit { outcome: wire::PolicyEditOutcome::Applied, revision: "r3".into(), current: None };
        assert!(check_edit::<()>(&revoked, &t, "fixture.read", "r", None).is_ok());
        let conflict = wire::PolicyEdit { outcome: wire::PolicyEditOutcome::Conflict, revision: "r2".into(), current: Some(policy_rule(false)) };
        assert!(check_edit::<()>(&conflict, &t, "fixture.read", "r", Some(true)).is_ok());
        let refused = wire::PolicyEdit { outcome: wire::PolicyEditOutcome::Forbidden, revision: "r2".into(), current: None };
        assert!(check_edit::<()>(&refused, &t, "fixture.read", "r", Some(true)).is_err());
    }
}
