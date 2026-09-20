//! Explicit content service binding. Resource naming remains unverified.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_storage_content_api as wire;
use std::{io::Write, time::Instant};
use wire::{ContentChanges, ContentReader, ContentWriter};
#[derive(Debug)]
pub enum Error<E> {
    Call(wire::CallError<E>),
    Invalid(&'static str),
    Outcome(String),
    Writer(std::io::Error),
}
#[derive(Debug)]
pub struct CopyError<E> {
    pub confirmed: u64,
    pub cause: Error<E>,
}
fn require<E>(ok: bool, s: &'static str) -> Result<(), Error<E>> {
    if ok {
        Ok(())
    } else {
        Err(Error::Invalid(s))
    }
}
fn digest(s: &str) -> bool {
    s.len() == 71
        && s.starts_with("sha256:")
        && s.as_bytes()[7..]
            .iter()
            .all(|c| matches!(c,b'0'..=b'9'|b'a'..=b'f'))
}
fn resource(r: &wire::Resource) -> bool {
    !r.handle.is_empty()
        && r.handle.len() <= 128
        && digest(&r.digest)
        && r.size >= 0
        && r.verification == "unverified"
}
#[derive(Clone)]
pub struct Client<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Client<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn open(&self, d: &str) -> Result<wire::OpenResult, Error<T::Error>> {
        require(digest(d), "canonical SHA256 digest")?;
        let r = wire::ContentReaderClient::new(self.transport.clone())
            .open(d.into())
            .map_err(Error::Call)?;
        require(
            (r.outcome == "opened") == r.resource.is_some(),
            "open outcome",
        )?;
        if let Some(v) = &r.resource {
            require(resource(v) && v.digest == d, "resource")?;
        }
        Ok(r)
    }
    pub fn read(
        &self,
        v: &wire::Resource,
        offset: i64,
        max_bytes: i64,
    ) -> Result<wire::ReadResult, Error<T::Error>> {
        require(
            resource(v) && offset >= 0 && offset <= v.size && (1..=65536).contains(&max_bytes),
            "read bounds",
        )?;
        let r = wire::ContentReaderClient::new(self.transport.clone())
            .read(v.handle.clone(), offset, max_bytes)
            .map_err(Error::Call)?;
        validate_read::<T::Error>(&r, v, offset, max_bytes)?;
        Ok(r)
    }
    pub fn close(&self, v: &wire::Resource) -> Result<wire::CloseResult, Error<T::Error>> {
        require(resource(v), "resource")?;
        wire::ContentReaderClient::new(self.transport.clone())
            .close(v.handle.clone())
            .map_err(Error::Call)
    }
    /// Streams unverified bytes. Caller closes resource and verifies assembled digest.
    fn copy_scoped<W: Write>(
        &self,
        v: &wire::Resource,
        out: &mut W,
    ) -> Result<u64, CopyError<T::Error>> {
        let mut confirmed = 0u64;
        let result = (|| loop {
            let r = self.read(v, confirmed as i64, 65536)?;
            if r.outcome != "data" {
                return Err(Error::Outcome(r.outcome.to_string()));
            }
            let c = r.chunk.unwrap();
            let mut rest = c.data.as_slice();
            while !rest.is_empty() {
                match out.write(rest) {
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
impl<T: wire::FrameTransport + Clone + abstraction_facade_service::ScopedTransport> Client<T> {
    /// One total waiting budget. Writer errors retain the confirmed byte count.
    pub fn copy<W: Write>(
        &self,
        value: &wire::Resource,
        out: &mut W,
    ) -> Result<u64, CopyError<T::Error>> {
        let transport = self.transport.call_scope().map_err(|error| CopyError {
            confirmed: 0,
            cause: Error::Call(wire::CallError::Transport(error)),
        })?;
        Client::new(transport).copy_scoped(value, out)
    }
}
/// Maximum bytes accepted by one Append.
pub const MAX_APPEND_BYTES: usize = 65536;
fn request_id(s: &str) -> bool {
    (16..=128).contains(&s.len())
        && s
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || c == b'_' || c == b'-')
}
fn upload(u: &wire::Upload) -> bool {
    !u.handle.is_empty()
        && u.handle.len() <= 128
        && digest(&u.digest)
        && u.size >= 0
        && u.received >= 0
        && u.received <= u.size
}
/// Fresh caller-retained request identity. Retain it before `begin` to
/// reconcile a lost reply; the service scopes it to the calling program.
pub fn new_request_id() -> std::io::Result<String> {
    let mut bytes = [0u8; 16];
    std::fs::File::open("/dev/urandom")
        .and_then(|mut f| std::io::Read::read_exact(&mut f, &mut bytes))
        .or_else(|_| windows_random(&mut bytes))?;
    Ok(bytes.iter().map(|b| format!("{b:02x}")).collect())
}
#[cfg(windows)]
fn windows_random(bytes: &mut [u8]) -> std::io::Result<()> {
    #[link(name = "advapi32")]
    extern "system" {
        #[link_name = "SystemFunction036"]
        fn rtl_gen_random(buffer: *mut u8, length: u32) -> u8;
    }
    if unsafe { rtl_gen_random(bytes.as_mut_ptr(), bytes.len() as u32) } == 0 {
        return Err(std::io::Error::other("system random source unavailable"));
    }
    Ok(())
}
#[cfg(not(windows))]
fn windows_random(_: &mut [u8]) -> std::io::Result<()> {
    Err(std::io::Error::other("system random source unavailable"))
}
/// Bounded authorized uploads through one content-writer binding.
#[derive(Clone)]
pub struct Writer<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Writer<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn begin(
        &self,
        request: &str,
        d: &str,
        size: i64,
    ) -> Result<wire::BeginResult, Error<T::Error>> {
        require(
            request_id(request) && digest(d) && size >= 0,
            "write request",
        )?;
        let r = wire::ContentWriterClient::new(self.transport.clone())
            .begin(request.into(), d.into(), size)
            .map_err(Error::Call)?;
        validate_begin::<T::Error>(&r, d, size)?;
        Ok(r)
    }
    pub fn append(
        &self,
        u: &wire::Upload,
        offset: i64,
        data: &[u8],
    ) -> Result<wire::AppendResult, Error<T::Error>> {
        require(
            upload(u) && offset >= 0 && (1..=MAX_APPEND_BYTES).contains(&data.len()),
            "append bounds",
        )?;
        let r = wire::ContentWriterClient::new(self.transport.clone())
            .append(u.handle.clone(), offset, data.to_vec())
            .map_err(Error::Call)?;
        validate_append::<T::Error>(&r, u, offset, data.len() as i64)?;
        Ok(r)
    }
    pub fn commit(&self, u: &wire::Upload) -> Result<wire::CommitResult, Error<T::Error>> {
        require(upload(u), "upload")?;
        let r = wire::ContentWriterClient::new(self.transport.clone())
            .commit(u.handle.clone())
            .map_err(Error::Call)?;
        validate_commit::<T::Error>(&r, u)?;
        Ok(r)
    }
    pub fn abort(&self, u: &wire::Upload) -> Result<wire::AbortResult, Error<T::Error>> {
        require(upload(u), "upload")?;
        wire::ContentWriterClient::new(self.transport.clone())
            .abort(u.handle.clone())
            .map_err(Error::Call)
    }
    fn write_scoped(
        &self,
        request: &str,
        d: &str,
        content: &[u8],
    ) -> Result<wire::Stored, Error<T::Error>> {
        let size = content.len() as i64;
        let begun = self.begin(request, d, size)?;
        match begun.outcome.as_str() {
            "committed" | "present" => return Ok(begun.stored.unwrap()),
            "started" => {}
            _ => return Err(Error::Outcome(begun.outcome.to_string())),
        }
        let u = begun.upload.unwrap();
        let mut offset = u.received;
        while offset < size {
            let end = (offset as usize + MAX_APPEND_BYTES).min(content.len());
            let a = self.append(&u, offset, &content[offset as usize..end])?;
            match a.outcome.as_str() {
                "accepted" | "out_of_order" => offset = a.received,
                _ => return Err(Error::Outcome(a.outcome.to_string())),
            }
        }
        let c = self.commit(&u)?;
        if c.outcome != "committed" {
            return Err(Error::Outcome(c.outcome.to_string()));
        }
        Ok(c.stored.unwrap())
    }
}
impl<T: wire::FrameTransport + Clone + abstraction_facade_service::ScopedTransport> Writer<T> {
    /// Uploads content under a caller-retained identity with one total waiting
    /// budget. Retrying the same identity resumes a live upload or returns its
    /// committed result. It never aborts.
    pub fn write(
        &self,
        request: &str,
        d: &str,
        content: &[u8],
    ) -> Result<wire::Stored, Error<T::Error>> {
        let transport = self
            .transport
            .call_scope()
            .map_err(|e| Error::Call(wire::CallError::Transport(e)))?;
        Writer::new(transport).write_scoped(request, d, content)
    }
}
impl<C: Connector> Writer<Binding<C>> {
    pub fn with_waiting(
        &self,
        deadline: Option<Instant>,
        cancellation: Option<C::Cancellation>,
    ) -> Result<Self, TransportError<C>> {
        Ok(Self::new(
            self.transport.with_waiting(deadline, cancellation)?,
        ))
    }
}
fn cursor(s: &str) -> bool {
    s.len() <= 256
}
/// Checks one change page against its request: refusals keep the cursor and
/// carry nothing; pages advance with ordered, well-formed entries.
pub fn check_change_page<E>(p: &wire::ChangePage, from: &str, max: i64) -> Result<(), Error<E>> {
    if p.outcome != "page" {
        return require(p.changes.is_empty() && p.next == from && !p.at_end, "change refusal");
    }
    require(p.changes.len() as i64 <= max && !p.next.is_empty() && cursor(&p.next), "change page")?;
    let mut last = 0;
    for c in &p.changes {
        require(
            c.sequence > last && digest(&c.digest) && c.size >= 0 && (c.kind == "added" || c.kind == "removed"),
            "change entry",
        )?;
        last = c.sequence;
    }
    Ok(())
}
/// Checks one snapshot page: refusals carry nothing; pages list ordered digests.
pub fn check_listing_page<E>(p: &wire::ListingPage, limit: i64) -> Result<(), Error<E>> {
    if p.outcome != "page" {
        return require(
            p.objects.is_empty() && p.continuation.is_empty() && p.cursor.is_empty() && !p.complete,
            "listing refusal",
        );
    }
    require(
        p.objects.len() as i64 <= limit && !p.cursor.is_empty() && p.complete == p.continuation.is_empty() && cursor(&p.continuation),
        "listing page",
    )?;
    let mut previous = "";
    for o in &p.objects {
        require(digest(&o.digest) && o.size >= 0 && o.digest.as_str() > previous, "listed object")?;
        previous = &o.digest;
    }
    Ok(())
}
/// Objects a store gains or loses through one content-changes binding. An
/// empty cursor starts at the current end; a gap requires rebuilding from
/// `snapshot`. The binding's waiting budget must cover `wait_ms`, and calls
/// are never retried.
#[derive(Clone)]
pub struct Changes<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Changes<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn observe(&self, from: &str, max_changes: i64, wait_ms: i64) -> Result<wire::ChangePage, Error<T::Error>> {
        require(
            cursor(from) && (1..=256).contains(&max_changes) && (0..=30000).contains(&wait_ms),
            "change request",
        )?;
        let p = wire::ContentChangesClient::new(self.transport.clone())
            .observe(from.into(), max_changes, wait_ms)
            .map_err(Error::Call)?;
        check_change_page::<T::Error>(&p, from, max_changes)?;
        Ok(p)
    }
    pub fn list(&self, continuation: &str, limit: i64) -> Result<wire::ListingPage, Error<T::Error>> {
        require(cursor(continuation) && (1..=256).contains(&limit), "listing request")?;
        let p = wire::ContentChangesClient::new(self.transport.clone())
            .list(continuation.into(), limit)
            .map_err(Error::Call)?;
        check_listing_page::<T::Error>(&p, limit)?;
        Ok(p)
    }
    /// Every object of one snapshot and the cursor to observe from. A refusal
    /// or gap is returned as `Error::Outcome`.
    pub fn snapshot(&self, limit: i64) -> Result<(Vec<wire::ListedObject>, String), Error<T::Error>> {
        let (mut objects, mut continuation, mut at) = (Vec::new(), String::new(), String::new());
        loop {
            let p = self.list(&continuation, limit)?;
            if p.outcome != "page" {
                return Err(Error::Outcome(p.outcome.to_string()));
            }
            require(at.is_empty() || p.cursor == at, "snapshot cursor changed between pages")?;
            at = p.cursor;
            objects.extend(p.objects);
            if p.complete {
                return Ok((objects, at));
            }
            continuation = p.continuation;
        }
    }
}
impl<C: Connector> Changes<Binding<C>> {
    pub fn with_waiting(
        &self,
        deadline: Option<Instant>,
        cancellation: Option<C::Cancellation>,
    ) -> Result<Self, TransportError<C>> {
        Ok(Self::new(self.transport.with_waiting(deadline, cancellation)?))
    }
}
pub trait StorageMachine<C: Connector> {
    /// Every Observe and List remains subject to the observe and per-object read policies.
    fn resolve_storage_changes(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Changes<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
    fn resolve_storage(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Client<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
    /// Every Begin, Append and Commit remains subject to the service write policy.
    fn resolve_storage_writer(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Writer<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> StorageMachine<C> for Machine<C> {
    fn resolve_storage_changes(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Changes<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Changes::new(self.resolve_service(
            "abstraction.storage/content-changes@1",
            g,
            s,
        )?))
    }
    fn resolve_storage(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Client<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Client::new(self.resolve_service(
            "abstraction.storage/content-reader@1",
            g,
            s,
        )?))
    }
    fn resolve_storage_writer(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Writer<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Writer::new(self.resolve_service(
            "abstraction.storage/content-writer@1",
            g,
            s,
        )?))
    }
}
fn validate_begin<E>(r: &wire::BeginResult, d: &str, size: i64) -> Result<(), Error<E>> {
    let stored_outcome = r.outcome == "committed" || r.outcome == "present";
    require(
        (r.outcome == "started") == r.upload.is_some()
            && stored_outcome == r.stored.is_some()
            && r.limit >= 0,
        "begin outcome",
    )?;
    if let Some(u) = &r.upload {
        require(upload(u) && u.digest == d && u.size == size, "upload")?;
    }
    if let Some(s) = &r.stored {
        require(
            s.digest == d
                && (r.outcome == "committed") == (s.evidence == "hashed")
                && (r.outcome != "committed" || s.size == size),
            "stored result",
        )?;
    }
    Ok(())
}
fn validate_append<E>(
    r: &wire::AppendResult,
    u: &wire::Upload,
    offset: i64,
    n: i64,
) -> Result<(), Error<E>> {
    let ok = match r.outcome.as_str() {
        "accepted" => r.received == offset + n && r.received <= u.size,
        "out_of_order" | "too_large" => r.received >= 0 && r.received <= u.size,
        _ => r.received == 0,
    };
    require(ok, "append result")
}
fn validate_commit<E>(r: &wire::CommitResult, u: &wire::Upload) -> Result<(), Error<E>> {
    require(
        (r.outcome == "committed") == r.stored.is_some()
            && r.stored.as_ref().is_none_or(|s| {
                s.digest == u.digest && s.size == u.size && s.evidence == "hashed"
            })
            && (r.outcome == "incomplete" || r.received == 0)
            && r.received >= 0
            && r.received <= u.size,
        "commit result",
    )
}
impl<C: Connector> Client<Binding<C>> {
    pub fn with_waiting(
        &self,
        deadline: Option<Instant>,
        cancellation: Option<C::Cancellation>,
    ) -> Result<Self, TransportError<C>> {
        Ok(Self::new(
            self.transport.with_waiting(deadline, cancellation)?,
        ))
    }
}
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

fn validate_read<E>(
    r: &wire::ReadResult,
    v: &wire::Resource,
    offset: i64,
    max_bytes: i64,
) -> Result<(), Error<E>> {
    require((r.outcome == "data") == r.chunk.is_some(), "read outcome")?;
    if let Some(c) = &r.chunk {
        let n = c.data.len() as i64;
        require(
            c.offset == offset
                && c.total == v.size
                && n <= max_bytes
                && n <= v.size - offset
                && c.eof == (n == v.size - offset)
                && (n > 0 || c.eof),
            "content chunk",
        )?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    const DIGEST: &str = "sha256:0000000000000000000000000000000000000000000000000000000000000000";
    #[test]
    fn change_and_listing_pages_keep_their_shapes() {
        let change = |sequence: i64| wire::Change {
            sequence,
            kind: wire::ChangeKind::Added,
            digest: DIGEST.into(),
            size: 0,
        };
        let empty = |outcome| wire::ChangePage { outcome, changes: vec![], next: String::new(), at_end: false };
        let mut page = empty(wire::ChangePageOutcome::Page);
        page.next = "c2".into();
        page.changes = vec![change(1), change(2)];
        assert!(check_change_page::<()>(&page, "c1", 2).is_ok());
        assert!(check_change_page::<()>(&page, "c1", 1).is_err());
        page.changes = vec![change(2), change(1)];
        assert!(check_change_page::<()>(&page, "c1", 2).is_err());
        let mut gap = empty(wire::ChangePageOutcome::Gap);
        gap.next = "c1".into();
        assert!(check_change_page::<()>(&gap, "c1", 16).is_ok());
        gap.next = "moved".into();
        assert!(check_change_page::<()>(&gap, "c1", 16).is_err());
        let mut listing = wire::ListingPage {
            outcome: wire::ListingOutcome::Forbidden,
            objects: vec![],
            continuation: String::new(),
            complete: false,
            cursor: String::new(),
        };
        assert!(check_listing_page::<()>(&listing, 16).is_ok());
        listing.cursor = "leaked".into();
        assert!(check_listing_page::<()>(&listing, 16).is_err());
        listing.outcome = wire::ListingOutcome::Page;
        listing.complete = true;
        let listed = wire::ListedObject { digest: DIGEST.into(), ..Default::default() };
        listing.objects = vec![listed];
        assert!(check_listing_page::<()>(&listing, 16).is_ok());
        listing.complete = false;
        assert!(check_listing_page::<()>(&listing, 16).is_err());
    }
    fn up() -> wire::Upload {
        wire::Upload {
            handle: "opaque".into(),
            digest: DIGEST.into(),
            size: 4,
            received: 0,
        }
    }
    fn st(evidence: &str, size: i64) -> Option<wire::Stored> {
        Some(wire::Stored {
            digest: DIGEST.into(),
            size,
            evidence: wire::Evidence::from_wire(evidence).unwrap(),
        })
    }
    fn begin(
        outcome: &str,
        upload: Option<wire::Upload>,
        stored: Option<wire::Stored>,
        limit: i64,
    ) -> wire::BeginResult {
        wire::BeginResult {
            outcome: wire::BeginOutcome::from_wire(outcome).unwrap(),
            upload,
            stored,
            limit,
        }
    }
    fn append(outcome: &str, received: i64) -> wire::AppendResult {
        wire::AppendResult {
            outcome: wire::AppendOutcome::from_wire(outcome).unwrap(),
            received,
        }
    }
    fn commit(outcome: &str, stored: Option<wire::Stored>, received: i64) -> wire::CommitResult {
        wire::CommitResult {
            outcome: wire::CommitOutcome::from_wire(outcome).unwrap(),
            stored,
            received,
        }
    }
    #[test]
    fn writer_rejects_inconsistent_results() {
        let u = up();
        assert!(validate_begin::<()>(&begin("started", Some(up()), None, 16), DIGEST, 4).is_ok());
        assert!(validate_begin::<()>(&begin("committed", None, st("hashed", 4), 16), DIGEST, 4).is_ok());
        assert!(validate_begin::<()>(&begin("present", None, st("named", 0), 16), DIGEST, 4).is_ok());
        let bad = [
            begin("started", None, None, 16),
            begin("started", Some(up()), None, -1),
            begin("started", Some(up()), st("hashed", 4), 16),
            begin("committed", None, st("named", 4), 16),
            begin("committed", None, st("hashed", 3), 16),
            begin("present", None, st("hashed", 0), 16),
            begin("forbidden", Some(up()), None, 0),
        ];
        for (i, r) in bad.iter().enumerate() {
            assert!(validate_begin::<()>(r, DIGEST, 4).is_err(), "begin case {i}");
        }
        assert!(validate_begin::<()>(&begin("started", Some(up()), None, 16), DIGEST, 5).is_err());
        assert!(validate_append::<()>(&append("accepted", 2), &u, 0, 2).is_ok());
        assert!(validate_append::<()>(&append("out_of_order", 1), &u, 2, 2).is_ok());
        for r in [append("accepted", 3), append("too_large", 5), append("gap", 1)] {
            assert!(validate_append::<()>(&r, &u, 0, 2).is_err());
        }
        assert!(validate_commit::<()>(&commit("committed", st("hashed", 4), 0), &u).is_ok());
        assert!(validate_commit::<()>(&commit("incomplete", None, 2), &u).is_ok());
        for r in [
            commit("committed", None, 0),
            commit("committed", st("named", 4), 0),
            commit("committed", st("hashed", 4), 1),
            commit("incomplete", None, 5),
            commit("gap", None, 1),
        ] {
            assert!(validate_commit::<()>(&r, &u).is_err());
        }
        assert!(request_id("abcdef0123456789"));
        assert!(!request_id("short") && !request_id("has space in it!!"));
        let id = new_request_id().unwrap();
        assert!(request_id(&id) && id.len() == 32 && id != new_request_id().unwrap());
    }
    #[test]
    fn rejects_inconsistent_chunks() {
        let v = wire::Resource {
            handle: "opaque".into(),
            digest: format!("sha256:{}", "0".repeat(64)),
            size: 2,
            verification: wire::Verification::Unverified,
        };
        let good = || wire::ReadResult {
            outcome: wire::ReadOutcome::Data,
            chunk: Some(wire::Chunk {
                offset: 0,
                total: 2,
                data: vec![1],
                eof: false,
            }),
        };
        assert!(validate_read::<()>(&good(), &v, 0, 1).is_ok());
        for which in 0..7 {
            let mut r = good();
            match which {
                0 => r.outcome = wire::ReadOutcome::Gap,
                1 => r.chunk = None,
                2 => r.chunk.as_mut().unwrap().offset = 1,
                3 => r.chunk.as_mut().unwrap().total = 3,
                4 => r.chunk.as_mut().unwrap().data.clear(),
                5 => r.chunk.as_mut().unwrap().eof = true,
                _ => r.chunk.as_mut().unwrap().data = vec![1, 2],
            }
            assert!(validate_read::<()>(&r, &v, 0, 1).is_err(), "case {which}");
        }
    }
}
