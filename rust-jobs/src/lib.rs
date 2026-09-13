//! Optional jobs semantics over the shared transport-independent binding.
pub use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub mod jobs;
pub use jobs::{Inventory, Jobs};
fn distinct(values: &[String]) -> bool {
    values
        .iter()
        .enumerate()
        .all(|(i, v)| !v.is_empty() && !values[..i].contains(v))
}
#[derive(Debug)]
pub enum Error<E> {
    Resolution(abstraction_facade_service::Error<E>),
    Job(jobs::Error<E>),
    Transport(E),
}
impl<E: std::fmt::Debug> std::fmt::Display for Error<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl<E: std::fmt::Debug> std::error::Error for Error<E> {}
pub trait JobsMachine<C: Connector> {
    fn resolve_jobs(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Jobs<Binding<C>>, Error<TransportError<C>>>;
    fn resolve_job_operations(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Jobs<Binding<C>>, Error<TransportError<C>>>;
    fn resolve_job_inventory(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Inventory<Binding<C>>, Error<TransportError<C>>>;
}
fn bind<C: Connector>(
    machine: &Machine<C>,
    contract: &str,
    g: Vec<String>,
    s: &str,
    receipt_requirements: bool,
) -> Result<Jobs<Binding<C>>, Error<TransportError<C>>> {
    let binding = machine
        .resolve_service(contract, g.clone(), s)
        .map_err(Error::Resolution)?
        .with_limit(2 * 1024 * 1024)
        .map_err(Error::Transport)?;
    Jobs::new(
        binding,
        String::new(),
        if receipt_requirements { g } else { vec![] },
    )
    .map_err(Error::Job)
}
impl<C: Connector> JobsMachine<C> for Machine<C> {
    fn resolve_jobs(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Jobs<Binding<C>>, Error<TransportError<C>>> {
        bind(self, "abstraction.job/acceptance@1", g, s, true)
    }
    fn resolve_job_operations(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Jobs<Binding<C>>, Error<TransportError<C>>> {
        bind(self, "abstraction.job/operations@1", g, s, true)
    }
    fn resolve_job_inventory(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Inventory<Binding<C>>, Error<TransportError<C>>> {
        Ok(Inventory(bind(
            self,
            "abstraction.job/inventory@1",
            g,
            s,
            false,
        )?))
    }
}
