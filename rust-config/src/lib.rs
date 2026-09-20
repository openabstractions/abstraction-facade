//! Resolved configuration reader, latest-snapshot observer and user-rung editor
//! with explicit edit outcomes.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_config_api as wire;
use wire::{ConfigEditor, ConfigObserver, ConfigReader};

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

fn empty(v: &wire::UserSettings) -> bool {
    v.nas_store.is_empty()
        && v.store.is_empty()
        && v.log_sink.is_empty()
        && v.log_service.is_empty()
        && v.off.is_empty()
}

/// Checks the invariants of a replacement result against its contract:
/// applied and conflict carry a snapshot with a revision; forbidden and
/// unavailable carry empty values and an empty revision and changed nothing.
pub fn check_replace<E>(r: &wire::UserReplaceResult) -> Result<(), Error<E>> {
    match r.outcome.as_str() {
        "applied" | "conflict" => require(!r.snapshot.revision.is_empty(), "snapshot revision"),
        "forbidden" | "unavailable" => require(
            r.snapshot.revision.is_empty() && empty(&r.snapshot.values),
            "refusal carries settings",
        ),
        _ => Err(Error::Invalid("replace outcome")),
    }
}

/// Edits the service user's configuration rung. Each call is one exchange with
/// no retry; an uncertain replacement is reconciled by rereading.
#[derive(Clone)]
pub struct Editor<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Editor<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn read_user(&self) -> Result<wire::UserSnapshot, Error<T::Error>> {
        let r = wire::ConfigEditorClient::new(self.transport.clone())
            .read_user()
            .map_err(Error::Call)?;
        require(!r.revision.is_empty(), "user revision")?;
        Ok(r)
    }
    /// `forbidden` is an evaluated edit-policy refusal; `unavailable` means the
    /// decision could not be obtained and the same edit may be retried.
    pub fn replace_user(
        &self,
        expected_revision: &str,
        values: wire::UserSettings,
    ) -> Result<wire::UserReplaceResult, Error<T::Error>> {
        require(!expected_revision.is_empty(), "expected revision required")?;
        let r = wire::ConfigEditorClient::new(self.transport.clone())
            .replace_user(expected_revision.into(), values)
            .map_err(Error::Call)?;
        check_replace(&r)?;
        Ok(r)
    }
}

fn overrides_bounded(v: &wire::RunOverrides) -> bool {
    [&v.nas_store, &v.store, &v.log_sink, &v.log_service].iter().all(|s| s.len() <= 4096)
}

/// Checks an observation against its contract: `snapshot` carries a snapshot
/// and a new cursor; `unchanged` and every refusal keep the supplied cursor and
/// carry no snapshot, and `unchanged` never answers an empty cursor.
pub fn check_observation<E>(r: &wire::ConfigObservation, cursor: &str) -> Result<(), Error<E>> {
    require(
        matches!(r.outcome.as_str(), "snapshot" | "unchanged" | "gap" | "unavailable" | "unsupported" | "invalid"),
        "observation outcome",
    )?;
    if r.outcome == "snapshot" {
        return require(
            r.snapshot.is_some() && !r.cursor.is_empty() && r.cursor != cursor && r.cursor.len() <= 512,
            "malformed observation snapshot",
        );
    }
    require(
        r.snapshot.is_none() && r.cursor == cursor && !(r.outcome == "unchanged" && cursor.is_empty()),
        "observation refusal changed cursor",
    )
}

/// Reads effective configuration with explicit run overrides. No writes or fallback.
#[derive(Clone)]
pub struct Reader<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Reader<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn read(&self, overrides: wire::RunOverrides) -> Result<wire::Snapshot, Error<T::Error>> {
        require(overrides_bounded(&overrides), "oversized override")?;
        wire::ConfigReaderClient::new(self.transport.clone())
            .read(overrides)
            .map_err(Error::Call)
    }
}

/// Latest-snapshot long-poll observation. Changed overrides need an empty
/// cursor; `gap` needs an explicit empty-cursor restart. The binding's waiting
/// budget must cover `wait_ms`, and calls are never retried.
#[derive(Clone)]
pub struct Observer<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Observer<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn observe(
        &self,
        overrides: wire::RunOverrides,
        cursor: &str,
        wait_ms: i64,
    ) -> Result<wire::ConfigObservation, Error<T::Error>> {
        require(
            (0..=30000).contains(&wait_ms) && cursor.len() <= 512 && overrides_bounded(&overrides),
            "invalid observation bounds",
        )?;
        let r = wire::ConfigObserverClient::new(self.transport.clone())
            .observe(overrides, cursor.into(), wait_ms)
            .map_err(Error::Call)?;
        check_observation::<T::Error>(&r, cursor)?;
        Ok(r)
    }
}

pub trait ConfigMachine<C: Connector> {
    /// Resolution grants no edit authority; the service's edit policy decides each replacement.
    fn resolve_config_editor(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Editor<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
    fn resolve_config(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Reader<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
    /// Requires the selected provider's notification contract.
    fn resolve_config_observer(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Observer<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> ConfigMachine<C> for Machine<C> {
    fn resolve_config(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Reader<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Reader::new(self.resolve_service("abstraction.config/reader@1", g, s)?))
    }
    fn resolve_config_observer(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Observer<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Observer::new(self.resolve_service("abstraction.config/observer@1", g, s)?))
    }
    fn resolve_config_editor(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Editor<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Editor::new(self.resolve_service(
            "abstraction.config/editor@1",
            g,
            s,
        )?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn observation(outcome: &str, cursor: &str, snapshot: bool) -> wire::ConfigObservation {
        wire::ConfigObservation {
            outcome: wire::ConfigObservationOutcome::from_wire(outcome).unwrap(),
            cursor: cursor.into(),
            snapshot: snapshot.then(wire::Snapshot::default),
        }
    }
    #[test]
    fn observations_keep_their_shapes() {
        assert!(check_observation::<()>(&observation("snapshot", "c2", true), "c1").is_ok());
        assert!(check_observation::<()>(&observation("snapshot", "c1", true), "c1").is_err());
        assert!(check_observation::<()>(&observation("snapshot", "c2", false), "c1").is_err());
        assert!(check_observation::<()>(&observation("unchanged", "c1", false), "c1").is_ok());
        assert!(check_observation::<()>(&observation("unchanged", "", false), "").is_err());
        assert!(check_observation::<()>(&observation("gap", "c1", false), "c1").is_ok());
        assert!(check_observation::<()>(&observation("gap", "moved", false), "c1").is_err());
        assert!(check_observation::<()>(&observation("unavailable", "c1", true), "c1").is_err());
    }
    fn result(outcome: &str, revision: &str, store: &str) -> wire::UserReplaceResult {
        let mut r = wire::UserReplaceResult {
            outcome: wire::UserReplaceOutcome::from_wire(outcome).unwrap(),
            snapshot: wire::UserSnapshot::default(),
        };
        r.snapshot.revision = revision.into();
        r.snapshot.values.store = store.into();
        r
    }
    #[test]
    fn replacement_outcomes_keep_their_shapes() {
        assert!(check_replace::<()>(&result("applied", "r1", "x")).is_ok());
        assert!(check_replace::<()>(&result("conflict", "r2", "")).is_ok());
        assert!(check_replace::<()>(&result("forbidden", "", "")).is_ok());
        assert!(check_replace::<()>(&result("unavailable", "", "")).is_ok());
        assert!(check_replace::<()>(&result("applied", "", "")).is_err());
        assert!(check_replace::<()>(&result("forbidden", "r1", "")).is_err());
        assert!(check_replace::<()>(&result("unavailable", "", "leaked")).is_err());
    }
}
