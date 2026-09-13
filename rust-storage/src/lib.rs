//! Explicit content service binding. Resource naming remains unverified.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_storage_content_api as wire;
use std::{io::Write, time::Instant};
use wire::ContentReader;
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
            .Open(d.into())
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
            .Read(v.handle.clone(), offset, max_bytes)
            .map_err(Error::Call)?;
        validate_read::<T::Error>(&r, v, offset, max_bytes)?;
        Ok(r)
    }
    pub fn close(&self, v: &wire::Resource) -> Result<wire::CloseResult, Error<T::Error>> {
        require(resource(v), "resource")?;
        wire::ContentReaderClient::new(self.transport.clone())
            .Close(v.handle.clone())
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
                return Err(Error::Outcome(r.outcome));
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
pub trait StorageMachine<C: Connector> {
    fn resolve_storage(
        &self,
        guarantees: Vec<String>,
        scope: &str,
    ) -> Result<Client<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> StorageMachine<C> for Machine<C> {
    fn resolve_storage(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Client<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Client::new(self.resolve_service(
            "abstraction.storage/content-reader@1",
            g,
            s,
        )?))
    }
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
    #[test]
    fn rejects_inconsistent_chunks() {
        let v = wire::Resource {
            handle: "opaque".into(),
            digest: format!("sha256:{}", "0".repeat(64)),
            size: 2,
            verification: "unverified".into(),
        };
        let good = || wire::ReadResult {
            outcome: "data".into(),
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
                0 => r.outcome = "gap".into(),
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
