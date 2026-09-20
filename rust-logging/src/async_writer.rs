//! A bounded asynchronous writer over a resolved logging sink [LOG-S12, LOG-S13].
//!
//! `write` queues a record and returns at once. One delivery thread hands the
//! queued records to the sink in order, through a writer thread that owns the
//! sink so a hung delivery can be abandoned after its write timeout. The queue
//! holds `capacity` records (1024), counting the record being delivered, and a
//! full queue drops the newest record and counts it. A failed delivery keeps its
//! record at the head, rebinds at once, then retries after 100 ms, doubling up to
//! 5 s with 20 % jitter, rebinding before each retry. A recovery first delivers
//! one WARN gap record naming the records dropped since the last delivery. The
//! first time the queue is empty after a recovery, another gap record names the
//! records dropped after the first was built. Records go to no other sink.
//!
//! `on_failure`, `on_recovery` and `on_overflow` run once per transition on the
//! thread that observed it. With none of them set, each transition is one line
//! on standard error.
use crate::logging::{Attestation, Record};
use std::collections::{BTreeMap, VecDeque};
use std::sync::mpsc::{channel, Receiver, RecvTimeoutError, Sender};
use std::sync::{Arc, Condvar, Mutex, MutexGuard};
use std::thread::JoinHandle;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

/// The message of the WARN record a recovery delivers.
pub const GAP_MESSAGE: &str = "records dropped while the sink was unreachable";

/// A failed delivery or rebind. `class` is the resolution status when a rebind
/// failed, `timeout` for a delivery that exceeded its write timeout, and
/// `write_failed` otherwise.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DeliveryError {
    pub class: String,
    pub message: String,
}

impl DeliveryError {
    pub fn write_failed(message: impl Into<String>) -> Self {
        Self {
            class: "write_failed".into(),
            message: message.into(),
        }
    }
    pub fn timeout(message: impl Into<String>) -> Self {
        Self {
            class: "timeout".into(),
            message: message.into(),
        }
    }
    /// The facade's resolution error, named by its status word.
    pub fn resolution(status: impl Into<String>, message: impl Into<String>) -> Self {
        Self {
            class: status.into(),
            message: message.into(),
        }
    }
}

impl std::fmt::Display for DeliveryError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for DeliveryError {}

/// A sink the writer thread owns.
pub trait Deliver: Send + 'static {
    fn deliver(&mut self, record: &Record) -> Result<(), DeliveryError>;
    /// Resolves the service again. The default keeps the same sink.
    fn rebind(&mut self) -> Result<(), DeliveryError> {
        Ok(())
    }
}

/// A snapshot. Every record handed to `write` is exactly one of accepted or
/// dropped; every accepted record ends as exactly one of written, failed or
/// abandoned, or is still queued.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Counts {
    pub accepted: u64,
    pub dropped: u64,
    pub written: u64,
    pub failed: u64,
    pub abandoned: u64,
    pub queued: usize,
    pub failing: bool,
    pub last_error: String,
}

pub type FailureCallback = Box<dyn Fn(&DeliveryError, &Counts) + Send + Sync>;
pub type CountsCallback = Box<dyn Fn(&Counts) + Send + Sync>;

pub struct AsyncOptions {
    pub capacity: usize,
    /// `None` leaves deliveries unbounded.
    pub write_timeout: Option<Duration>,
    pub initial_backoff: Duration,
    pub max_backoff: Duration,
    /// Names the hop 0 claim of the gap record and of `log`.
    pub program: String,
    pub on_failure: Option<FailureCallback>,
    pub on_recovery: Option<CountsCallback>,
    pub on_overflow: Option<CountsCallback>,
}

impl Default for AsyncOptions {
    fn default() -> Self {
        Self {
            capacity: 1024,
            write_timeout: Some(Duration::from_secs(5)),
            initial_backoff: Duration::from_millis(100),
            max_backoff: Duration::from_secs(5),
            program: String::new(),
            on_failure: None,
            on_recovery: None,
            on_overflow: None,
        }
    }
}

enum Request {
    Deliver(Record),
    Rebind,
}

enum Reply {
    Done(Result<(), DeliveryError>),
    Stopped,
}

#[derive(Default)]
struct State {
    queue: VecDeque<Record>,
    counts: Counts,
    closed: bool,
    stop: bool,
    finished: bool,
    overflowing: bool,
    /// Dropped records no delivered gap record reported.
    lost: u64,
    /// The dropped count the latest gap record carries.
    reported: u64,
    /// A recovery happened and the queue has not been empty since.
    draining: bool,
    /// The current failure began, or the previous gap record's until while draining.
    since: Option<SystemTime>,
    /// The until of the latest gap record built.
    until: Option<SystemTime>,
}

struct Shared {
    state: Mutex<State>,
    changed: Condvar,
    options: AsyncOptions,
}

impl Shared {
    fn lock(&self) -> MutexGuard<'_, State> {
        self.state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
    }
}

fn snapshot(state: &State) -> Counts {
    Counts {
        queued: state.queue.len(),
        ..state.counts.clone()
    }
}

/// The bounded asynchronous writer.
pub struct AsyncWriter {
    shared: Arc<Shared>,
    wake: Sender<Reply>,
    delivery: Option<JoinHandle<()>>,
}

impl AsyncWriter {
    pub fn new<D: Deliver>(sink: D, options: AsyncOptions) -> Self {
        Self::reporting(sink, options, Arc::new(|line: &str| eprintln!("{line}")))
    }

    /// `new` with the default reporter's lines sent to `report`.
    fn reporting<D: Deliver>(
        sink: D,
        mut options: AsyncOptions,
        report: Arc<dyn Fn(&str) + Send + Sync>,
    ) -> Self {
        if options.capacity == 0 {
            options.capacity = 1024;
        }
        if options.on_failure.is_none()
            && options.on_recovery.is_none()
            && options.on_overflow.is_none()
        {
            let (failure, overflow, recovery) = (Arc::clone(&report), Arc::clone(&report), report);
            options.on_failure = Some(Box::new(move |error, c| {
                failure(&format!(
                    "abstraction.logging: service unreachable ({}); {} queued, {} dropped",
                    error.class, c.queued, c.dropped
                ))
            }));
            options.on_overflow = Some(Box::new(move |c| {
                overflow(&format!(
                    "abstraction.logging: queue full, dropping newest records; {} queued, {} dropped",
                    c.queued, c.dropped
                ))
            }));
            options.on_recovery = Some(Box::new(move |c| {
                recovery(&format!(
                    "abstraction.logging: delivering again; {} dropped",
                    c.dropped
                ))
            }));
        }
        let shared = Arc::new(Shared {
            state: Mutex::new(State::default()),
            changed: Condvar::new(),
            options,
        });
        let (requests, incoming) = channel::<Request>();
        let (replies, answers) = channel::<Reply>();
        let wake = replies.clone();
        std::thread::Builder::new()
            .name("abstraction.logging writer".into())
            .spawn(move || writer(sink, incoming, replies))
            .expect("spawn the logging writer thread");
        let delivery_shared = Arc::clone(&shared);
        let delivery = std::thread::Builder::new()
            .name("abstraction.logging delivery".into())
            .spawn(move || {
                Delivery {
                    shared: delivery_shared,
                    requests,
                    answers,
                    pending: 0,
                }
                .run()
            })
            .expect("spawn the logging delivery thread");
        Self {
            shared,
            wake,
            delivery: Some(delivery),
        }
    }

    /// Queues the record. Returns false when it was dropped or the writer is closed.
    pub fn write(&self, record: Record) -> bool {
        let mut state = self.shared.lock();
        if state.closed {
            return false;
        }
        if state.queue.len() < self.shared.options.capacity {
            state.queue.push_back(record);
            state.counts.accepted += 1;
            self.shared.changed.notify_all();
            return true;
        }
        state.counts.dropped += 1;
        state.lost += 1;
        if state.overflowing {
            return false;
        }
        state.overflowing = true;
        let counts = snapshot(&state);
        drop(state);
        if let Some(callback) = &self.shared.options.on_overflow {
            callback(&counts);
        }
        false
    }

    /// Builds a record with the writer's hop 0 claim and queues it.
    pub fn log(&self, level: i64, message: &str, attrs: BTreeMap<String, String>) -> bool {
        self.write(Record {
            schema: 1,
            time: instant(SystemTime::now()),
            level,
            msg: message.into(),
            identity: vec![claim(&self.shared.options.program)],
            attrs,
            ..Default::default()
        })
    }

    pub fn counts(&self) -> Counts {
        snapshot(&self.shared.lock())
    }

    /// Stops accepting records and delivers the queue until `timeout`. At the
    /// timeout the record being delivered is counted failed and the records
    /// behind it abandoned. Without a write timeout, a delivery in progress keeps
    /// running and the counts settle when it returns.
    pub fn close(&mut self, timeout: Duration) -> Counts {
        let deadline = Instant::now() + timeout;
        let mut state = self.shared.lock();
        state.closed = true;
        self.shared.changed.notify_all();
        while !state.finished {
            let now = Instant::now();
            if now >= deadline {
                break;
            }
            state = self
                .shared
                .changed
                .wait_timeout(state, deadline - now)
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .0;
        }
        if !state.finished {
            state.stop = true;
            self.shared.changed.notify_all();
            drop(state);
            let _ = self.wake.send(Reply::Stopped);
            if self.shared.options.write_timeout.is_none() {
                return self.counts();
            }
            state = self.shared.lock();
            while !state.finished {
                state = self
                    .shared
                    .changed
                    .wait(state)
                    .unwrap_or_else(|poisoned| poisoned.into_inner());
            }
        }
        let counts = snapshot(&state);
        drop(state);
        if let Some(delivery) = self.delivery.take() {
            let _ = delivery.join();
        }
        counts
    }
}

impl Drop for AsyncWriter {
    fn drop(&mut self) {
        let timeout = self.shared.options.write_timeout.unwrap_or_default();
        self.close(timeout);
        if let Some(delivery) = self.delivery.take() {
            let _ = delivery.join();
        }
    }
}

fn writer<D: Deliver>(mut sink: D, incoming: Receiver<Request>, replies: Sender<Reply>) {
    for request in incoming {
        let result = match request {
            Request::Deliver(record) => sink.deliver(&record),
            Request::Rebind => sink.rebind(),
        };
        if replies.send(Reply::Done(result)).is_err() {
            return;
        }
    }
}

struct Delivery {
    shared: Arc<Shared>,
    requests: Sender<Request>,
    answers: Receiver<Reply>,
    /// Requests whose reply timed out and has not arrived.
    pending: usize,
}

impl Delivery {
    fn run(mut self) {
        loop {
            if let Err(error) = self.report_drained() {
                if self.stopped() || !self.retry(error) {
                    let mut state = self.shared.lock();
                    state.counts.abandoned += state.queue.len() as u64;
                    state.queue.clear();
                    return self.finish(state);
                }
                continue;
            }
            let record = {
                let mut state = self.shared.lock();
                loop {
                    if state.stop {
                        state.counts.abandoned += state.queue.len() as u64;
                        state.queue.clear();
                        return self.finish(state);
                    }
                    if let Some(record) = state.queue.front() {
                        break record.clone();
                    }
                    if state.closed {
                        return self.finish(state);
                    }
                    state = self
                        .shared
                        .changed
                        .wait(state)
                        .unwrap_or_else(|poisoned| poisoned.into_inner());
                }
            };
            match self.ask(Request::Deliver(record)) {
                Ok(()) => {
                    let mut state = self.shared.lock();
                    Self::pop(&mut state);
                    state.counts.written += 1;
                    if !state.draining {
                        state.lost = 0;
                    }
                    self.shared.changed.notify_all();
                }
                Err(error) => {
                    if self.stopped() || !self.retry(error) {
                        let mut state = self.shared.lock();
                        if !state.queue.is_empty() {
                            Self::pop(&mut state);
                            state.counts.failed += 1;
                        }
                        state.counts.abandoned += state.queue.len() as u64;
                        state.queue.clear();
                        return self.finish(state);
                    }
                }
            }
        }
    }

    /// The first time the queue is empty after a recovery, delivers a gap record
    /// for the records dropped after the recovery's gap record was built
    /// [LOG-S13]. Returns that delivery's error.
    fn report_drained(&mut self) -> Result<(), DeliveryError> {
        {
            let mut state = self.shared.lock();
            if !state.draining || !state.queue.is_empty() {
                return Ok(());
            }
            if state.lost == 0 {
                state.draining = false;
                return Ok(());
            }
        }
        let gap = self.gap_record();
        self.ask(Request::Deliver(gap))?;
        let mut state = self.shared.lock();
        state.lost -= state.reported;
        state.since = state.until;
        Ok(())
    }

    fn finish(&self, mut state: MutexGuard<'_, State>) {
        state.finished = true;
        self.shared.changed.notify_all();
    }

    fn stopped(&self) -> bool {
        self.shared.lock().stop
    }

    fn pop(state: &mut State) {
        state.queue.pop_front();
        if state.queue.is_empty() {
            state.overflowing = false;
        }
    }

    /// Sends one request to the writer thread and waits for its reply within the
    /// write timeout, after the replies of earlier timed-out requests.
    fn ask(&mut self, request: Request) -> Result<(), DeliveryError> {
        let timeout = self.shared.options.write_timeout;
        while self.pending > 0 {
            match self.receive(timeout) {
                Some(Reply::Done(_)) => self.pending -= 1,
                Some(Reply::Stopped) => return Err(DeliveryError::write_failed("abstraction.logging: writer closed")),
                None => {
                    return Err(DeliveryError::timeout(
                        "abstraction.logging: delivery exceeded its write timeout: an earlier delivery has not returned",
                    ))
                }
            }
        }
        if self.requests.send(request).is_err() {
            return Err(DeliveryError::write_failed(
                "abstraction.logging: writer thread ended",
            ));
        }
        match self.receive(timeout) {
            Some(Reply::Done(result)) => result,
            Some(Reply::Stopped) => {
                self.pending += 1;
                Err(DeliveryError::write_failed(
                    "abstraction.logging: writer closed",
                ))
            }
            None => {
                self.pending += 1;
                Err(DeliveryError::timeout(format!(
                    "abstraction.logging: delivery exceeded its write timeout ({} ms)",
                    timeout.unwrap_or_default().as_millis()
                )))
            }
        }
    }

    fn receive(&self, timeout: Option<Duration>) -> Option<Reply> {
        match timeout {
            None => self.answers.recv().ok(),
            Some(timeout) => match self.answers.recv_timeout(timeout) {
                Ok(reply) => Some(reply),
                Err(RecvTimeoutError::Timeout) => None,
                Err(RecvTimeoutError::Disconnected) => Some(Reply::Stopped),
            },
        }
    }

    fn retry(&mut self, delivery_error: DeliveryError) -> bool {
        {
            let mut state = self.shared.lock();
            state.counts.failing = true;
            state.counts.last_error = delivery_error.message.clone();
            if !state.draining {
                state.since = Some(SystemTime::now());
            }
            self.shared.changed.notify_all();
        }
        let cause = self.ask(Request::Rebind).err().unwrap_or(delivery_error);
        let counts = {
            let mut state = self.shared.lock();
            state.counts.last_error = cause.message.clone();
            snapshot(&state)
        };
        if let Some(callback) = &self.shared.options.on_failure {
            callback(&cause, &counts);
        }
        let mut delay = self.shared.options.initial_backoff;
        loop {
            {
                let state = self.shared.lock();
                let pause = delay.mul_f64(0.8 + 0.4 * jitter());
                let (state, _) = self
                    .shared
                    .changed
                    .wait_timeout_while(state, pause, |s| !s.stop)
                    .unwrap_or_else(|poisoned| poisoned.into_inner());
                if state.stop {
                    return false;
                }
            }
            delay = (delay * 2).min(self.shared.options.max_backoff);
            let result = self.ask(Request::Rebind).and_then(|()| {
                let gap = self.gap_record();
                self.ask(Request::Deliver(gap))
            });
            if self.stopped() {
                return false;
            }
            match result {
                Err(error) => self.shared.lock().counts.last_error = error.message,
                Ok(()) => {
                    let counts = {
                        let mut state = self.shared.lock();
                        state.counts.failing = false;
                        state.lost -= state.reported;
                        state.since = state.until;
                        state.draining = true;
                        snapshot(&state)
                    };
                    if let Some(callback) = &self.shared.options.on_recovery {
                        callback(&counts);
                    }
                    return true;
                }
            }
        }
    }

    fn gap_record(&self) -> Record {
        let (since, lost, until) = {
            let mut state = self.shared.lock();
            let until = SystemTime::now();
            state.reported = state.lost;
            state.until = Some(until);
            (state.since.unwrap_or(until), state.lost, until)
        };
        Record {
            schema: 1,
            time: instant(until),
            level: 4,
            msg: GAP_MESSAGE.into(),
            identity: vec![claim(&self.shared.options.program)],
            attrs: BTreeMap::from([
                ("dropped".to_string(), lost.to_string()),
                ("since".to_string(), instant(since)),
                ("until".to_string(), instant(until)),
            ]),
            ..Default::default()
        }
    }
}

/// A number in [0, 1) that varies between calls, without a random-number crate.
fn jitter() -> f64 {
    use std::hash::{BuildHasher, Hasher};
    let mut hasher = std::collections::hash_map::RandomState::new().build_hasher();
    hasher.write_u128(
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos(),
    );
    (hasher.finish() >> 11) as f64 / (1u64 << 53) as f64
}

fn claim(program: &str) -> Attestation {
    Attestation {
        by: "self".into(),
        program: program.into(),
        uid: -1,
        gid: -1,
        pid: -1,
        ..Default::default()
    }
}

/// A fixed-width UTC instant with microseconds [LOG-R7].
pub fn instant(at: SystemTime) -> String {
    let since = at.duration_since(UNIX_EPOCH).unwrap_or_default();
    let seconds = since.as_secs() as i64;
    let (days, rest) = (seconds.div_euclid(86_400), seconds.rem_euclid(86_400));
    // Howard Hinnant's civil_from_days.
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = yoe + era * 400 + i64::from(month <= 2);
    format!(
        "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}.{:06}Z",
        rest / 3600,
        rest % 3600 / 60,
        rest % 60,
        since.subsec_micros()
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    /// A JSON value, enough to read the fixture.
    #[derive(Clone, Debug)]
    enum Json {
        Null,
        Bool(bool),
        Number(f64),
        Text(String),
        List(Vec<Json>),
        Object(Vec<(String, Json)>),
    }

    impl Json {
        fn get(&self, key: &str) -> Option<&Json> {
            match self {
                Json::Object(members) => members.iter().find(|(k, _)| k == key).map(|(_, v)| v),
                _ => None,
            }
        }
        fn at(&self, key: &str) -> &Json {
            self.get(key)
                .unwrap_or_else(|| panic!("fixture has no member {key}"))
        }
        fn text(&self) -> &str {
            match self {
                Json::Text(t) => t,
                other => panic!("not text: {other:?}"),
            }
        }
        fn number(&self) -> f64 {
            match self {
                Json::Number(n) => *n,
                other => panic!("not a number: {other:?}"),
            }
        }
        fn flag(&self) -> bool {
            matches!(self, Json::Bool(true))
        }
        fn items(&self) -> &[Json] {
            match self {
                Json::List(items) => items,
                other => panic!("not a list: {other:?}"),
            }
        }
        fn texts(&self) -> Vec<String> {
            self.items().iter().map(|j| j.text().to_string()).collect()
        }
    }

    fn parse(text: &str) -> Json {
        fn space(b: &[u8], i: &mut usize) {
            while *i < b.len() && b[*i].is_ascii_whitespace() {
                *i += 1;
            }
        }
        fn string(b: &[u8], i: &mut usize) -> String {
            *i += 1;
            let mut out = Vec::new();
            while b[*i] != b'"' {
                if b[*i] == b'\\' {
                    *i += 1;
                }
                out.push(b[*i]);
                *i += 1;
            }
            *i += 1;
            String::from_utf8(out).expect("utf-8 fixture")
        }
        fn value(b: &[u8], i: &mut usize) -> Json {
            space(b, i);
            match b[*i] {
                b'{' => {
                    *i += 1;
                    let mut members = Vec::new();
                    loop {
                        space(b, i);
                        if b[*i] == b'}' {
                            *i += 1;
                            return Json::Object(members);
                        }
                        let key = string(b, i);
                        space(b, i);
                        *i += 1;
                        members.push((key, value(b, i)));
                        space(b, i);
                        if b[*i] == b',' {
                            *i += 1;
                        }
                    }
                }
                b'[' => {
                    *i += 1;
                    let mut items = Vec::new();
                    loop {
                        space(b, i);
                        if b[*i] == b']' {
                            *i += 1;
                            return Json::List(items);
                        }
                        items.push(value(b, i));
                        space(b, i);
                        if b[*i] == b',' {
                            *i += 1;
                        }
                    }
                }
                b'"' => Json::Text(string(b, i)),
                b't' => {
                    *i += 4;
                    Json::Bool(true)
                }
                b'f' => {
                    *i += 5;
                    Json::Bool(false)
                }
                b'n' => {
                    *i += 4;
                    Json::Null
                }
                _ => {
                    let start = *i;
                    while *i < b.len() && (b[*i] == b'-' || b[*i] == b'.' || b[*i].is_ascii_digit())
                    {
                        *i += 1;
                    }
                    Json::Number(
                        std::str::from_utf8(&b[start..*i])
                            .expect("digits")
                            .parse()
                            .expect("number"),
                    )
                }
            }
        }
        let mut i = 0;
        value(text.as_bytes(), &mut i)
    }

    #[derive(Default)]
    struct Outage {
        down: bool,
        delivered: Vec<Record>,
        rebinds: usize,
        write_error: String,
        status: String,
        cause: String,
        /// A held message's next successful delivery waits until release.
        hold: Option<String>,
        held: bool,
        release: bool,
    }

    #[derive(Clone)]
    struct OutageSink(Arc<Mutex<Outage>>);

    impl Deliver for OutageSink {
        fn deliver(&mut self, record: &Record) -> Result<(), DeliveryError> {
            let mut outage = self.0.lock().expect("outage");
            if outage.down {
                return Err(DeliveryError::write_failed(outage.write_error.clone()));
            }
            if outage.hold.as_deref() == Some(record.msg.as_str()) {
                outage.hold = None;
                outage.held = true;
                while !outage.release {
                    drop(outage);
                    std::thread::sleep(Duration::from_millis(1));
                    outage = self.0.lock().expect("outage");
                }
            }
            outage.delivered.push(record.clone());
            Ok(())
        }
        fn rebind(&mut self) -> Result<(), DeliveryError> {
            let mut outage = self.0.lock().expect("outage");
            outage.rebinds += 1;
            if outage.down {
                return Err(DeliveryError::resolution(
                    outage.status.clone(),
                    format!(
                        "service resolution: {}: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime: {}",
                        outage.status, outage.cause
                    ),
                ));
            }
            Ok(())
        }
    }

    impl OutageSink {
        fn set(&self, down: bool) {
            self.0.lock().expect("outage").down = down;
        }
        fn rebinds(&self) -> usize {
            self.0.lock().expect("outage").rebinds
        }
        fn delivered(&self) -> Vec<Record> {
            self.0.lock().expect("outage").delivered.clone()
        }
    }

    fn outage_sink() -> OutageSink {
        OutageSink(Arc::new(Mutex::new(Outage {
            write_error: "down".into(),
            status: "runtime_unavailable".into(),
            cause: "down".into(),
            ..Default::default()
        })))
    }

    fn eventually(what: &str, condition: impl Fn() -> bool) {
        let deadline = Instant::now() + Duration::from_secs(10);
        while !condition() {
            assert!(Instant::now() < deadline, "timed out waiting for {what}");
            std::thread::sleep(Duration::from_millis(1));
        }
    }

    fn counts_of(c: &Counts) -> (u64, u64, u64, u64, u64, usize, bool) {
        (
            c.accepted,
            c.dropped,
            c.written,
            c.failed,
            c.abandoned,
            c.queued,
            c.failing,
        )
    }

    fn counts_from(j: &Json) -> (u64, u64, u64, u64, u64, usize, bool) {
        let n = |k: &str| j.at(k).number() as u64;
        (
            n("accepted"),
            n("dropped"),
            n("written"),
            n("failed"),
            n("abandoned"),
            n("queued") as usize,
            j.at("failing").flag(),
        )
    }

    fn fixture() -> Option<Json> {
        let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("../../../conformance/faults/sink-outage/sink-outage.json");
        std::fs::read_to_string(path).ok().map(|text| parse(&text))
    }

    fn run_fixture(f: &Json, reporter: bool) {
        let options = f.at("options");
        let sink = OutageSink(Arc::new(Mutex::new(Outage {
            write_error: f.at("outage").at("write_error").text().into(),
            status: f.at("outage").at("rebind_error").at("status").text().into(),
            cause: f.at("outage").at("rebind_error").at("cause").text().into(),
            ..Default::default()
        })));
        let transitions = Arc::new(Mutex::new(Vec::<String>::new()));
        let failure = Arc::new(Mutex::new(None::<DeliveryError>));
        let lines = Arc::new(Mutex::new(Vec::<String>::new()));
        let mut async_options = AsyncOptions {
            capacity: options.at("capacity").number() as usize,
            initial_backoff: Duration::from_millis(options.at("initial_backoff_ms").number() as u64),
            max_backoff: Duration::from_millis(options.at("max_backoff_ms").number() as u64),
            program: options.at("program").text().into(),
            ..Default::default()
        };
        if !reporter {
            let (t1, t2, t3, seen) = (
                Arc::clone(&transitions),
                Arc::clone(&transitions),
                Arc::clone(&transitions),
                Arc::clone(&failure),
            );
            async_options.on_failure = Some(Box::new(move |error, _| {
                *seen.lock().expect("failure") = Some(error.clone());
                t1.lock().expect("transitions").push("failure".into());
            }));
            async_options.on_overflow = Some(Box::new(move |_| {
                t2.lock().expect("transitions").push("overflow".into())
            }));
            async_options.on_recovery = Some(Box::new(move |_| {
                t3.lock().expect("transitions").push("recovery".into())
            }));
        }
        let captured = Arc::clone(&lines);
        let mut writer = AsyncWriter::reporting(
            sink.clone(),
            async_options,
            Arc::new(move |line: &str| captured.lock().expect("lines").push(line.to_string())),
        );
        let reported = || {
            if reporter {
                lines.lock().expect("lines").len()
            } else {
                transitions.lock().expect("transitions").len()
            }
        };
        let mut last = Counts::default();
        for (index, step) in f.at("steps").items().iter().enumerate() {
            if let Some(messages) = step.get("log") {
                for message in messages.texts() {
                    writer.log(0, &message, BTreeMap::new());
                }
            } else if let Some(down) = step.get("outage") {
                sink.set(down.flag());
            } else if let Some(hold) = step.get("hold") {
                let mut outage = sink.0.lock().expect("outage");
                outage.hold = Some(hold.text().to_string());
                outage.release = false;
            } else if step.get("release").is_some() {
                sink.0.lock().expect("outage").release = true;
            } else if let Some(counts) = step.get("counts") {
                assert_eq!(
                    counts_of(&writer.counts()),
                    counts_from(counts),
                    "step {index}"
                );
            } else if step.get("close").is_some() {
                last = writer.close(Duration::from_secs(5));
            } else {
                match step.at("await").text() {
                    "settled" => eventually("settled", || {
                        let c = writer.counts();
                        c.queued == 0
                            && !c.failing
                            && c.written + c.failed + c.abandoned == c.accepted
                    }),
                    "failing" => eventually("the failure report", || {
                        writer.counts().failing && reported() >= 1
                    }),
                    "retry" => eventually("a retry during the outage", || sink.rebinds() >= 2),
                    "held" => {
                        eventually("the held delivery", || sink.0.lock().expect("outage").held)
                    }
                    other => panic!("unknown await {other}"),
                }
            }
        }
        let expect = f.at("expect");
        assert_eq!(counts_of(&last), counts_from(expect.at("counts")));
        assert!(
            last.last_error
                .contains(expect.at("last_error_contains").text()),
            "last error {}",
            last.last_error
        );
        let delivered = sink.delivered();
        assert_eq!(
            delivered.iter().map(|r| r.msg.clone()).collect::<Vec<_>>(),
            expect.at("delivered").texts()
        );
        let (gap, want) = (&delivered[2], expect.at("gap"));
        assert_eq!(
            (gap.level, gap.msg.as_str()),
            (want.at("level").number() as i64, want.at("msg").text())
        );
        assert_eq!(gap.identity.len(), 1);
        let writer_claim = &gap.identity[0];
        assert_eq!(
            (
                writer_claim.hop,
                writer_claim.by.as_str(),
                writer_claim.program.as_str()
            ),
            (
                want.at("hop").number() as i64,
                want.at("by").text(),
                want.at("program").text()
            )
        );
        if let Json::Object(attrs) = want.at("attrs") {
            for (key, value) in attrs {
                assert_eq!(
                    gap.attrs.get(key).map(String::as_str),
                    Some(value.text()),
                    "gap attribute {key}"
                );
            }
        }
        for key in want.at("timestamps").texts() {
            let value = &gap.attrs[&key];
            assert!(
                value.len() == 27 && value.ends_with('Z') && value.as_bytes()[19] == b'.',
                "gap instant {key} = {value}"
            );
        }
        assert!(gap.attrs["since"] <= gap.attrs["until"]);
        // Every drop is reported by exactly one gap record; a later gap record
        // begins where the previous one ended.
        let gaps: Vec<&Record> = delivered
            .iter()
            .filter(|r| r.msg == want.at("msg").text())
            .collect();
        assert_eq!(
            gaps.iter()
                .map(|r| r.attrs["dropped"].clone())
                .collect::<Vec<_>>(),
            expect.at("gaps_dropped").texts()
        );
        let reported_total: u64 = gaps
            .iter()
            .map(|r| r.attrs["dropped"].parse::<u64>().expect("dropped count"))
            .sum();
        assert_eq!(reported_total, last.dropped, "gap records against counts");
        for pair in gaps.windows(2) {
            assert_eq!(pair[1].attrs["since"], pair[0].attrs["until"]);
        }
        for r in &gaps {
            for key in want.at("timestamps").texts() {
                let value = &r.attrs[&key];
                assert!(
                    value.len() == 27 && value.ends_with('Z') && value.as_bytes()[19] == b'.',
                    "gap instant {key} = {value}"
                );
            }
            assert!(r.attrs["since"] <= r.attrs["until"]);
        }
        if reporter {
            assert_eq!(*lines.lock().expect("lines"), expect.at("reporter").texts());
        } else {
            assert!(lines.lock().expect("lines").is_empty());
            assert_eq!(
                *transitions.lock().expect("transitions"),
                expect.at("transitions").texts()
            );
            let failure = failure.lock().expect("failure").clone().expect("a failure");
            assert_eq!(failure.class, expect.at("failure_status").text());
        }
    }

    /// conformance/faults/sink-outage, which every language runs [LOG-S12, LOG-S13].
    #[test]
    fn sink_outage_fixture() {
        let Some(f) = fixture() else {
            eprintln!("conformance/faults/sink-outage is not beside this crate; skipped");
            return;
        };
        run_fixture(&f, false);
        run_fixture(&f, true);
    }

    /// A 2,000-record burst while the service is down drops exactly the newest
    /// 2000 - 1024, reports the overflow once, and the gap record carries the count.
    #[test]
    fn burst_during_an_outage_drops_the_newest_and_the_gap_carries_the_count() {
        let sink = outage_sink();
        let overflows = Arc::new(Mutex::new(0));
        let seen = Arc::clone(&overflows);
        let mut writer = AsyncWriter::new(
            sink.clone(),
            AsyncOptions {
                initial_backoff: Duration::from_millis(5),
                max_backoff: Duration::from_millis(20),
                on_overflow: Some(Box::new(move |_| *seen.lock().expect("overflows") += 1)),
                on_failure: Some(Box::new(|_, _| {})),
                ..Default::default()
            },
        );
        sink.set(true);
        for i in 0..2000 {
            writer.log(0, &format!("b{i:04}"), BTreeMap::new());
        }
        let during = writer.counts();
        assert_eq!(
            (during.accepted, during.dropped, during.queued),
            (1024, 976, 1024)
        );
        eventually("a retry during the outage", || {
            writer.counts().failing && sink.rebinds() >= 2
        });
        sink.set(false);
        let last = writer.close(Duration::from_secs(10));
        assert_eq!((last.written, last.failed, last.dropped), (1024, 0, 976));
        let delivered = sink.delivered();
        assert_eq!(delivered.len(), 1025);
        assert_eq!(
            (
                delivered[0].msg.as_str(),
                delivered[0].attrs["dropped"].as_str()
            ),
            (GAP_MESSAGE, "976")
        );
        assert_eq!(
            (delivered[1].msg.as_str(), delivered[1024].msg.as_str()),
            ("b0000", "b1023")
        );
        assert_eq!(*overflows.lock().expect("overflows"), 1);
    }

    /// With no callbacks, an outage and its recovery are exactly two lines, and
    /// no record is among them.
    #[test]
    fn without_callbacks_an_outage_is_two_reporter_lines() {
        let sink = outage_sink();
        sink.set(true);
        let lines = Arc::new(Mutex::new(Vec::<String>::new()));
        let captured = Arc::clone(&lines);
        let mut writer = AsyncWriter::reporting(
            sink.clone(),
            AsyncOptions {
                initial_backoff: Duration::from_millis(5),
                max_backoff: Duration::from_millis(10),
                ..Default::default()
            },
            Arc::new(move |line: &str| captured.lock().expect("lines").push(line.into())),
        );
        for i in 0..3 {
            writer.log(0, &format!("secret record {i}"), BTreeMap::new());
        }
        eventually("a retry", || sink.rebinds() >= 3);
        sink.set(false);
        let last = writer.close(Duration::from_secs(5));
        assert_eq!(
            *lines.lock().expect("lines"),
            vec![
                "abstraction.logging: service unreachable (runtime_unavailable); 3 queued, 0 dropped".to_string(),
                "abstraction.logging: delivering again; 0 dropped".to_string(),
            ]
        );
        assert_eq!((last.written, last.failed), (3, 0));
    }

    /// Close at its deadline during an outage counts the head record failed and
    /// the rest abandoned.
    #[test]
    fn close_at_the_deadline_during_an_outage_counts_abandoned() {
        let sink = outage_sink();
        sink.set(true);
        let mut writer = AsyncWriter::new(
            sink.clone(),
            AsyncOptions {
                initial_backoff: Duration::from_millis(5),
                max_backoff: Duration::from_millis(10),
                on_failure: Some(Box::new(|_, _| {})),
                ..Default::default()
            },
        );
        for i in 0..5 {
            writer.log(0, &format!("a{i}"), BTreeMap::new());
        }
        eventually("the failure", || writer.counts().failing);
        let began = Instant::now();
        let last = writer.close(Duration::from_millis(100));
        assert!(began.elapsed() < Duration::from_secs(2));
        assert_eq!(counts_of(&last), (5, 0, 0, 1, 4, 0, true));
    }

    /// A hung sink costs `write` nothing; the delivery times out as a failure of
    /// class timeout, and the records are delivered once the sink returns.
    #[test]
    fn a_hung_sink_times_out_and_delivers_once_it_returns() {
        #[derive(Clone)]
        struct Gate(Arc<(Mutex<bool>, Condvar)>, Arc<Mutex<Vec<String>>>);
        impl Deliver for Gate {
            fn deliver(&mut self, record: &Record) -> Result<(), DeliveryError> {
                let (open, signal) = &*self.0;
                let mut open = open.lock().expect("gate");
                while !*open {
                    open = signal.wait(open).expect("gate");
                }
                self.1.lock().expect("delivered").push(record.msg.clone());
                Ok(())
            }
        }
        let gate = Gate(
            Arc::new((Mutex::new(false), Condvar::new())),
            Arc::new(Mutex::new(Vec::new())),
        );
        let failure = Arc::new(Mutex::new(None::<DeliveryError>));
        let seen = Arc::clone(&failure);
        let mut writer = AsyncWriter::new(
            gate.clone(),
            AsyncOptions {
                write_timeout: Some(Duration::from_millis(30)),
                initial_backoff: Duration::from_millis(1),
                max_backoff: Duration::from_millis(2),
                on_failure: Some(Box::new(move |error, _| {
                    *seen.lock().expect("failure") = Some(error.clone())
                })),
                ..Default::default()
            },
        );
        let mut latencies = Vec::new();
        for i in 0..100 {
            let began = Instant::now();
            writer.log(0, &format!("h{i}"), BTreeMap::new());
            latencies.push(began.elapsed());
        }
        latencies.sort();
        // The 95th percentile, so one scheduler preemption on a loaded host does not decide it.
        assert!(
            latencies[95] < Duration::from_millis(1),
            "write 95th percentile {:?}",
            latencies[95]
        );
        eventually("the timeout failure", || {
            failure.lock().expect("failure").is_some()
        });
        assert_eq!(
            failure
                .lock()
                .expect("failure")
                .as_ref()
                .map(|e| e.class.as_str()),
            Some("timeout")
        );
        let during = writer.counts();
        assert_eq!((during.failed, during.queued), (0, 100));
        {
            let (open, signal) = &*gate.0;
            *open.lock().expect("gate") = true;
            signal.notify_all();
        }
        let last = writer.close(Duration::from_secs(10));
        assert_eq!((last.written, last.failed, last.failing), (100, 0, false));
    }

    #[test]
    fn instants_are_fixed_width() {
        assert_eq!(instant(UNIX_EPOCH), "1970-01-01T00:00:00.000000Z");
        assert_eq!(
            instant(UNIX_EPOCH + Duration::from_micros(1_789_000_123_456_789)),
            "2026-09-10T00:28:43.456789Z"
        );
    }
}
