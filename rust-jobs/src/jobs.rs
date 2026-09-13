//! Fixed provider binding. Transport errors leave acceptance unknown; no retry is issued.
pub use abstraction_job_api as wire;
use std::{
    io::Write,
    sync::{Arc, Mutex},
};
use wire::{JobInventory, OperationControl, RecoverableAcceptance};
#[derive(Debug)]
pub enum Error<E> {
    Call(wire::CallError<E>),
    Transport(E),
    Invalid(&'static str),
    Outcome(String),
    Writer(std::io::Error),
}
#[derive(Debug)]
pub struct CopyError<E> {
    pub confirmed: u64,
    pub cause: Error<E>,
}
fn require<E>(ok: bool, reason: &'static str) -> Result<(), Error<E>> {
    if ok {
        Ok(())
    } else {
        Err(Error::Invalid(reason))
    }
}
fn id(v: &wire::RequestIdentity) -> wire::RequestIdentity {
    wire::RequestIdentity {
        key: v.key.clone(),
        history_epoch: v.history_epoch.clone(),
    }
}
fn identity<E>(v: &wire::RequestIdentity) -> Result<(), Error<E>> {
    require(
        !v.key.is_empty() && !v.history_epoch.is_empty(),
        "request identity",
    )
}
#[derive(Clone)]
pub struct Jobs<T> {
    transport: T,
    owner: Arc<Mutex<String>>,
    required: Vec<String>,
}
impl<T: wire::FrameTransport + Clone> Jobs<T> {
    pub fn new(
        transport: T,
        expected_owner: String,
        required: Vec<String>,
    ) -> Result<Self, Error<T::Error>> {
        require(super::distinct(&required), "required guarantees")?;
        Ok(Self {
            transport,
            owner: Arc::new(Mutex::new(expected_owner)),
            required,
        })
    }
    pub fn owner(&self) -> String {
        self.owner.lock().unwrap().clone()
    }
    fn pin(&self, owner: &str) -> Result<(), Error<T::Error>> {
        let mut current = self.owner.lock().unwrap();
        require(
            !owner.is_empty() && (current.is_empty() || *current == owner),
            "logical owner",
        )?;
        *current = owner.into();
        Ok(())
    }
    fn receipt(
        &self,
        r: &wire::Receipt,
        wanted: &wire::RequestIdentity,
        required: &[String],
    ) -> Result<(), Error<T::Error>> {
        identity(wanted)?;
        require(
            r.identity.key == wanted.key
                && r.identity.history_epoch == wanted.history_epoch
                && !r.operation_id.is_empty()
                && !r.logical_owner.is_empty()
                && r.history_retention_ms > 0
                && super::distinct(&r.accepted_guarantees)
                && required.iter().all(|g| r.accepted_guarantees.contains(g)),
            "receipt",
        )
    }
    fn acceptance(
        &self,
        r: wire::AcceptanceResult,
        wanted: &wire::RequestIdentity,
        required: &[String],
    ) -> Result<wire::AcceptanceResult, Error<T::Error>> {
        if r.outcome == "accepted" {
            let receipt = r
                .receipt
                .as_ref()
                .ok_or(Error::Invalid("missing receipt"))?;
            self.receipt(receipt, wanted, required)?;
            self.pin(&receipt.logical_owner)?;
        } else {
            require(
                matches!(
                    r.outcome.as_str(),
                    "definitely_not_accepted"
                        | "unknown"
                        | "key_conflict"
                        | "forbidden"
                        | "invalid"
                ) && r.receipt.is_none(),
                "acceptance outcome",
            )?;
        }
        Ok(r)
    }
    pub fn history_window(&self) -> Result<wire::HistoryWindow, Error<T::Error>> {
        let r = wire::RecoverableAcceptanceClient::new(self.transport.clone())
            .GetHistoryWindow()
            .map_err(Error::Call)?;
        require(
            !r.history_epoch.is_empty() && r.minimum_retention_ms > 0,
            "history window",
        )?;
        self.pin(&r.logical_owner)?;
        Ok(r)
    }
    pub fn submit(
        &self,
        mut submission: wire::Submission,
    ) -> Result<wire::AcceptanceResult, Error<T::Error>> {
        identity(&submission.identity)?;
        require(
            !submission.kind.is_empty() && super::distinct(&submission.required_guarantees),
            "submission",
        )?;
        for g in &self.required {
            if !submission.required_guarantees.contains(g) {
                submission.required_guarantees.push(g.clone());
            }
        }
        let wanted = id(&submission.identity);
        let required = submission.required_guarantees.clone();
        let r = wire::RecoverableAcceptanceClient::new(self.transport.clone())
            .Submit(submission)
            .map_err(Error::Call)?;
        self.acceptance(r, &wanted, &required)
    }
    pub fn reconcile(
        &self,
        wanted: &wire::RequestIdentity,
    ) -> Result<wire::AcceptanceResult, Error<T::Error>> {
        identity(wanted)?;
        let r = wire::RecoverableAcceptanceClient::new(self.transport.clone())
            .Reconcile(id(wanted))
            .map_err(Error::Call)?;
        self.acceptance(r, wanted, &self.required)
    }
    pub fn cancel_work(
        &self,
        wanted: &wire::RequestIdentity,
    ) -> Result<wire::CancellationResult, Error<T::Error>> {
        identity(wanted)?;
        wire::RecoverableAcceptanceClient::new(self.transport.clone())
            .CancelWork(id(wanted))
            .map_err(Error::Call)
    }
    fn snapshot(
        &self,
        s: &wire::OperationSnapshot,
        wanted: &wire::RequestIdentity,
        required: &[String],
    ) -> Result<(), Error<T::Error>> {
        require(
            matches!(
                s.state.as_str(),
                "pending" | "running" | "transferred" | "complete" | "failed" | "cancelled"
            ) && s.progress.done >= 0
                && s.progress.total >= 0
                && s.failure.as_ref().map_or(true, |f| {
                    matches!(
                        f.classification.as_str(),
                        "retryable" | "permanent" | "unknown"
                    )
                }),
            "observation",
        )?;
        self.receipt(&s.receipt, wanted, required)
    }
    pub fn observe(
        &self,
        wanted: &wire::RequestIdentity,
    ) -> Result<wire::ObservationResult, Error<T::Error>> {
        identity(wanted)?;
        let r = wire::OperationControlClient::new(self.transport.clone())
            .ObserveWork(id(wanted))
            .map_err(Error::Call)?;
        if r.outcome == "observed" {
            let s = r
                .snapshot
                .as_ref()
                .ok_or(Error::Invalid("missing snapshot"))?;
            self.snapshot(s, wanted, &self.required)?;
            self.pin(&s.receipt.logical_owner)?;
        } else {
            require(
                matches!(
                    r.outcome.as_str(),
                    "unknown" | "forbidden" | "invalid" | "definitely_not_accepted"
                ) && r.snapshot.is_none(),
                "observation outcome",
            )?;
        }
        Ok(r)
    }
    pub fn read_result(
        &self,
        wanted: &wire::RequestIdentity,
        offset: i64,
        max_bytes: i64,
    ) -> Result<wire::ResultRead, Error<T::Error>> {
        identity(wanted)?;
        require(
            offset >= 0 && (1..=65536).contains(&max_bytes),
            "result range",
        )?;
        let r = wire::OperationControlClient::new(self.transport.clone())
            .ReadResult(id(wanted), offset, max_bytes)
            .map_err(Error::Call)?;
        if r.outcome == "data" {
            let c = r.chunk.as_ref().ok_or(Error::Invalid("missing chunk"))?;
            require(c.offset == offset && c.total >= offset, "chunk range")?;
            let n = c.data.len() as i64;
            require(
                n <= max_bytes
                    && n <= c.total - offset
                    && c.eof == (n == c.total - offset)
                    && (n > 0 || c.eof),
                "chunk bounds",
            )?;
            self.receipt(&c.receipt, wanted, &self.required)?;
            self.pin(&c.receipt.logical_owner)?;
        } else {
            require(
                matches!(
                    r.outcome.as_str(),
                    "not_ready"
                        | "unavailable"
                        | "unsupported"
                        | "unknown"
                        | "forbidden"
                        | "invalid"
                ) && r.chunk.is_none(),
                "result outcome",
            )?;
        }
        Ok(r)
    }
    pub fn copy_result<W: Write>(
        &self,
        wanted: &wire::RequestIdentity,
        destination: &mut W,
    ) -> Result<u64, CopyError<T::Error>>
    where
        T: abstraction_facade_service::ScopedTransport,
    {
        let transport = self.transport.call_scope().map_err(|cause| CopyError {
            confirmed: 0,
            cause: Error::Transport(cause),
        })?;
        Jobs {
            transport,
            owner: self.owner.clone(),
            required: self.required.clone(),
        }
        .copy_scoped(wanted, destination)
    }
    fn copy_scoped<W: Write>(
        &self,
        wanted: &wire::RequestIdentity,
        destination: &mut W,
    ) -> Result<u64, CopyError<T::Error>> {
        let mut confirmed = 0u64;
        let mut saved = None;
        let result = (|| loop {
            let r = self.read_result(wanted, confirmed as i64, 65536)?;
            if r.outcome != "data" {
                return Err(Error::Outcome(r.outcome));
            }
            let c = r.chunk.unwrap();
            let signature = (c.receipt.operation_id, c.total);
            if let Some(old) = &saved {
                require(*old == signature, "result changed")?;
            } else {
                saved = Some(signature);
            }
            let mut rest = c.data.as_slice();
            while !rest.is_empty() {
                match destination.write(rest) {
                    Ok(0) => return Err(Error::Writer(std::io::ErrorKind::WriteZero.into())),
                    Ok(n) if n <= rest.len() => {
                        confirmed += n as u64;
                        rest = &rest[n..];
                    }
                    Ok(_) => return Err(Error::Invalid("writer count")),
                    Err(e) => return Err(Error::Writer(e)),
                }
            }
            if c.eof {
                return Ok(confirmed);
            }
        })();
        result.map_err(|cause| CopyError { confirmed, cause })
    }
}
/// Inventory is a separate contract; older receipts need not carry new read guarantees.
pub struct Inventory<T>(pub(crate) Jobs<T>);
impl<T: wire::FrameTransport + Clone> Inventory<T> {
    pub fn new(transport: T, owner: String) -> Result<Self, Error<T::Error>> {
        Ok(Self(Jobs::new(transport, owner, vec![])?))
    }
    pub fn list(&self, cursor: &str, limit: i64) -> Result<wire::InventoryPage, Error<T::Error>> {
        require(
            cursor.len() <= 128 && (1..=64).contains(&limit),
            "inventory request",
        )?;
        let p = wire::JobInventoryClient::new(self.0.transport.clone())
            .ListWork(cursor.into(), limit)
            .map_err(Error::Call)?;
        if p.outcome != "page" {
            require(
                matches!(
                    p.outcome.as_str(),
                    "gap" | "forbidden" | "invalid" | "unavailable"
                ) && p.snapshots.is_empty()
                    && p.next.is_empty()
                    && !p.complete,
                "inventory outcome",
            )?;
            return Ok(p);
        }
        require(
            p.snapshots.len() <= limit as usize
                && p.next.len() <= 128
                && if p.complete {
                    p.next.is_empty()
                } else {
                    !p.next.is_empty() && p.next != cursor
                },
            "inventory continuation",
        )?;
        let mut seen = std::collections::HashSet::new();
        let mut owner = None;
        for s in &p.snapshots {
            self.0.snapshot(s, &s.receipt.identity, &[])?;
            require(
                seen.insert(&s.receipt.operation_id)
                    && owner.map_or(true, |o| o == &s.receipt.logical_owner),
                "inventory receipts",
            )?;
            owner = Some(&s.receipt.logical_owner);
        }
        if let Some(o) = owner {
            self.0.pin(o)?;
        }
        Ok(p)
    }
}

impl<C: super::Connector> Jobs<super::Binding<C>> {
    /// Restore caller-retained binding using its trusted connector, without discovery.
    pub fn restore(
        connector: C,
        endpoint: &str,
        owner: String,
        required: Vec<String>,
        deadline: Option<std::time::Instant>,
        cancellation: Option<C::Cancellation>,
    ) -> Result<Self, Error<super::TransportError<C>>> {
        require(!owner.is_empty(), "retained owner")?;
        let binding =
            super::Binding::restore(connector, endpoint, deadline, cancellation, 2 * 1024 * 1024)
                .map_err(|e| Error::Call(wire::CallError::Transport(e)))?;
        Self::new(binding, owner, required)
    }
    pub fn endpoint(&self) -> &str {
        self.transport.endpoint()
    }
    /// Fresh waiting policy through the shared binding; logical ownership stays pinned.
    pub fn with_waiting(
        &self,
        deadline: Option<std::time::Instant>,
        cancellation: Option<C::Cancellation>,
    ) -> Result<Self, Error<super::TransportError<C>>> {
        let binding = self
            .transport
            .with_waiting(deadline, cancellation)
            .map_err(|e| Error::Call(wire::CallError::Transport(e)))?;
        let mut result = Self::new(binding, self.owner(), self.required.clone())?;
        result.owner = self.owner.clone();
        Ok(result)
    }
}

#[cfg(test)]
mod tests;

impl<E: std::fmt::Debug> std::fmt::Display for Error<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl<E: std::fmt::Debug> std::error::Error for Error<E> {}
impl<E: std::fmt::Debug> std::fmt::Display for CopyError<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "after {} confirmed bytes: {}",
            self.confirmed, self.cause
        )
    }
}
impl<E: std::fmt::Debug> std::error::Error for CopyError<E> {}
impl<C: super::Connector> Inventory<super::Binding<C>> {
    pub fn endpoint(&self) -> &str {
        self.0.endpoint()
    }
    pub fn owner(&self) -> String {
        self.0.owner()
    }
    pub fn with_waiting(
        &self,
        deadline: Option<std::time::Instant>,
        cancellation: Option<C::Cancellation>,
    ) -> Result<Self, Error<super::TransportError<C>>> {
        Ok(Self(self.0.with_waiting(deadline, cancellation)?))
    }
}
