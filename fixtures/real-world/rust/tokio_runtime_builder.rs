// Source: https://github.com/tokio-rs/tokio/blob/master/tokio/src/runtime/builder.rs | License: MIT
#![cfg_attr(loom, allow(unused_imports))]

use crate::runtime::handle::Handle;
use crate::runtime::{
    blocking, driver, Callback, HistogramBuilder, Runtime, TaskCallback, TimerFlavor,
};
#[cfg(tokio_unstable)]
use crate::runtime::{metrics::HistogramConfiguration, TaskMeta};

use crate::runtime::{LocalOptions, LocalRuntime};
use crate::util::rand::{RngSeed, RngSeedGenerator};

use crate::runtime::blocking::BlockingPool;
use crate::runtime::scheduler::CurrentThread;
use std::fmt;
use std::io;
use std::thread::ThreadId;
use std::time::Duration;

/// Builds Tokio Runtime with custom configuration values.
///
/// Methods can be chained in order to set the configuration values. The
/// Runtime is constructed by calling [`build`].
///
/// New instances of `Builder` are obtained via [`Builder::new_multi_thread`]
/// or [`Builder::new_current_thread`].
///
/// See function level documentation for details on the various configuration
/// settings.
///
/// [`build`]: method@Self::build
/// [`Builder::new_multi_thread`]: method@Self::new_multi_thread
/// [`Builder::new_current_thread`]: method@Self::new_current_thread
///
/// # Examples
///
/// ```
/// # #[cfg(not(target_family = "wasm"))]
/// # {
/// use tokio::runtime::Builder;
///
/// fn main() {
///     // build runtime
///     let runtime = Builder::new_multi_thread()
///         .worker_threads(4)
///         .thread_name("my-custom-name")
///         .thread_stack_size(3 * 1024 * 1024)
///         .build()
///         .unwrap();
///
///     // use runtime ...
/// }
/// # }
/// ```
pub struct Builder {
    /// Runtime type
    kind: Kind,

    /// Name of the runtime.
    name: Option<String>,

    /// Whether or not to enable the I/O driver
    enable_io: bool,
    nevents: usize,

    /// Whether or not to enable the time driver
    enable_time: bool,

    /// Whether or not the clock should start paused.
    start_paused: bool,

    /// The number of worker threads, used by Runtime.
    ///
    /// Only used when not using the current-thread executor.
    worker_threads: Option<usize>,

    /// Cap on thread usage.
    max_blocking_threads: usize,

    /// Name fn used for threads spawned by the runtime.
    pub(super) thread_name: ThreadNameFn,

    /// Stack size used for threads spawned by the runtime.
    pub(super) thread_stack_size: Option<usize>,

    /// Callback to run after each thread starts.
    pub(super) after_start: Option<Callback>,

    /// To run before each worker thread stops
    pub(super) before_stop: Option<Callback>,

    /// To run before each worker thread is parked.
    pub(super) before_park: Option<Callback>,

    /// To run after each thread is unparked.
    pub(super) after_unpark: Option<Callback>,

    /// To run before each task is spawned.
    pub(super) before_spawn: Option<TaskCallback>,

    /// To run before each poll
    #[cfg(tokio_unstable)]
    pub(super) before_poll: Option<TaskCallback>,

    /// To run after each poll
    #[cfg(tokio_unstable)]
    pub(super) after_poll: Option<TaskCallback>,

    /// To run after each task is terminated.
    pub(super) after_termination: Option<TaskCallback>,

    /// Customizable keep alive timeout for `BlockingPool`
    pub(super) keep_alive: Option<Duration>,

    /// How many ticks before pulling a task from the global/remote queue?
    ///
    /// When `None`, the value is unspecified and behavior details are left to
    /// the scheduler. Each scheduler flavor could choose to either pick its own
    /// default value or use some other strategy to decide when to poll from the
    /// global queue. For example, the multi-threaded scheduler uses a
    /// self-tuning strategy based on mean task poll times.
    pub(super) global_queue_interval: Option<u32>,

    /// How many ticks before yielding to the driver for timer and I/O events?
    pub(super) event_interval: u32,

    /// When true, the multi-threade scheduler LIFO slot should not be used.
    ///
    /// This option should only be exposed as unstable.
    pub(super) disable_lifo_slot: bool,

    /// Specify a random number generator seed to provide deterministic results
    pub(super) seed_generator: RngSeedGenerator,

    /// When true, enables task poll count histogram instrumentation.
    pub(super) metrics_poll_count_histogram_enable: bool,

    /// Configures the task poll count histogram
    pub(super) metrics_poll_count_histogram: HistogramBuilder,

    #[cfg(tokio_unstable)]
    pub(super) unhandled_panic: UnhandledPanic,

    timer_flavor: TimerFlavor,

    /// Whether or not to enable eager hand-off for the I/O and time drivers (in
    /// `tokio_unstable`).
    enable_eager_driver_handoff: bool,
}

cfg_unstable! {
    /// How the runtime should respond to unhandled panics.
    ///
    /// Instances of `UnhandledPanic` are passed to `Builder::unhandled_panic`
    /// to configure the runtime behavior when a spawned task panics.
    ///
    /// See [`Builder::unhandled_panic`] for more details.
    #[derive(Debug, Clone)]
    #[non_exhaustive]
    pub enum UnhandledPanic {
        /// The runtime should ignore panics on spawned tasks.
        ///
        /// The panic is forwarded to the task's [`JoinHandle`] and all spawned
        /// tasks continue running normally.
        ///
        /// This is the default behavior.
        ///
        /// # Examples
        ///
        /// ```
        /// # #[cfg(not(target_family = "wasm"))]
        /// # {
        /// use tokio::runtime::{self, UnhandledPanic};
        ///
        /// # pub fn main() {
        /// let rt = runtime::Builder::new_current_thread()
        ///     .unhandled_panic(UnhandledPanic::Ignore)
        ///     .build()
        ///     .unwrap();
        ///
        /// let task1 = rt.spawn(async { panic!("boom"); });
        /// let task2 = rt.spawn(async {
        ///     // This task completes normally
        ///     "done"
        /// });
        ///
        /// rt.block_on(async {
        ///     // The panic on the first task is forwarded to the `JoinHandle`
        ///     assert!(task1.await.is_err());
        ///
        ///     // The second task completes normally
        ///     assert!(task2.await.is_ok());
        /// })
        /// # }
        /// # }
        /// ```
        ///
        /// [`JoinHandle`]: struct@crate::task::JoinHandle
        Ignore,

        /// The runtime should immediately shutdown if a spawned task panics.
        ///
        /// The runtime will immediately shutdown even if the panicked task's
        /// [`JoinHandle`] is still available. All further spawned tasks will be
        /// immediately dropped and call to [`Runtime::block_on`] will panic.
        ///
        /// # Examples
        ///
        /// ```should_panic
        /// use tokio::runtime::{self, UnhandledPanic};
        ///
        /// # pub fn main() {
        /// let rt = runtime::Builder::new_current_thread()
        ///     .unhandled_panic(UnhandledPanic::ShutdownRuntime)
        ///     .build()
        ///     .unwrap();
        ///
        /// rt.spawn(async { panic!("boom"); });
        /// rt.spawn(async {
        ///     // This task never completes.
        /// });
        ///
        /// rt.block_on(async {
        ///     // Do some work
        /// # loop { tokio::task::yield_now().await; }
        /// })
        /// # }
        /// ```
        ///
        /// [`JoinHandle`]: struct@crate::task::JoinHandle
        ShutdownRuntime,
    }
}

pub(crate) type ThreadNameFn = std::sync::Arc<dyn Fn() -> String + Send + Sync + 'static>;

#[derive(Clone, Copy)]
pub(crate) enum Kind {
    CurrentThread,
    #[cfg(feature = "rt-multi-thread")]
    MultiThread,
}

impl Builder {
    /// Returns a new builder with the current thread scheduler selected.
    ///
    /// Configuration methods can be chained on the return value.
    ///
    /// To spawn non-`Send` tasks on the resulting runtime, combine it with a
    /// [`LocalSet`], or call [`build_local`] to create a [`LocalRuntime`].
    ///
    /// [`LocalSet`]: crate::task::LocalSet
    /// [`LocalRuntime`]: crate::runtime::LocalRuntime
    /// [`build_local`]: crate::runtime::Builder::build_local
    pub fn new_current_thread() -> Builder {
        #[cfg(loom)]
        const EVENT_INTERVAL: u32 = 4;
        // The number `61` is fairly arbitrary. I believe this value was copied from golang.
        #[cfg(not(loom))]
        const EVENT_INTERVAL: u32 = 61;

        Builder::new(Kind::CurrentThread, EVENT_INTERVAL)
    }

    /// Returns a new builder with the multi thread scheduler selected.
    ///
    /// Configuration methods can be chained on the return value.
    #[cfg(feature = "rt-multi-thread")]
    #[cfg_attr(docsrs, doc(cfg(feature = "rt-multi-thread")))]
    pub fn new_multi_thread() -> Builder {
        // The number `61` is fairly arbitrary. I believe this value was copied from golang.
        Builder::new(Kind::MultiThread, 61)
    }

    /// Returns a new runtime builder initialized with default configuration
    /// values.
    ///
    /// Configuration methods can be chained on the return value.
    pub(crate) fn new(kind: Kind, event_interval: u32) -> Builder {
        Builder {
            kind,

            // Default runtime name
            name: None,

            // I/O defaults to "off"
            enable_io: false,
            nevents: 1024,

            // Time defaults to "off"
            enable_time: false,

            // The clock starts not-paused
            start_paused: false,

            // Read from environment variable first in multi-threaded mode.
            // Default to lazy auto-detection (one thread per CPU core)
            worker_threads: None,

            max_blocking_threads: 512,

            // Default thread name
            thread_name: std::sync::Arc::new(|| "tokio-rt-worker".into()),

            // Do not set a stack size by default
            thread_stack_size: None,

            // No worker thread callbacks
            after_start: None,
            before_stop: None,
            before_park: None,
            after_unpark: None,

            before_spawn: None,
            after_termination: None,

            #[cfg(tokio_unstable)]
            before_poll: None,
            #[cfg(tokio_unstable)]
            after_poll: None,

            keep_alive: None,

            // Defaults for these values depend on the scheduler kind, so we get them
            // as parameters.
            global_queue_interval: None,
            event_interval,

            seed_generator: RngSeedGenerator::new(RngSeed::new()),

            #[cfg(tokio_unstable)]
            unhandled_panic: UnhandledPanic::Ignore,

            metrics_poll_count_histogram_enable: false,

            metrics_poll_count_histogram: HistogramBuilder::default(),

            disable_lifo_slot: false,

            timer_flavor: TimerFlavor::Traditional,

            // Eager driver handoff is disabled by default.
            enable_eager_driver_handoff: false,
        }
    }

    /// Enables both I/O and time drivers.
    ///
    /// Doing this is a shorthand for calling `enable_io` and `enable_time`
    /// individually. If additional components are added to Tokio in the future,
    /// `enable_all` will include these future components.
    ///
    /// # Examples
    ///
    /// ```
    /// # #[cfg(not(target_family = "wasm"))]
    /// # {
    /// use tokio::runtime;
    ///
    /// let rt = runtime::Builder::new_multi_thread()
    ///     .enable_all()
    ///     .build()
    ///     .unwrap();
    /// # }
    /// ```
    pub fn enable_all(&mut self) -> &mut Self {
        #[cfg(any(
            feature = "net",
            all(unix, feature = "process"),
            all(unix, feature = "signal")
        ))]
        self.enable_io();

        #[cfg(all(
            tokio_unstable,
            feature = "io-uring",
            feature = "rt",
            feature = "fs",
            target_os = "linux",
        ))]
        self.enable_io_uring();

        #[cfg(feature = "time")]
        self.enable_time();

        self
    }

    /// Enables the alternative timer implementation, which is disabled by default.
    ///
    /// The alternative timer implementation is an unstable feature that may
    /// provide better performance on multi-threaded runtimes with a large number
    /// of worker threads.
    ///
    /// This option only applies to multi-threaded runtimes. Attempting to use
    /// this option with any other runtime type will have no effect.
    ///
    /// [Click here to share your experience with the alternative timer](https://github.com/tokio-rs/tokio/issues/7745)
    ///
    /// # Examples
    ///
    /// ```
    /// # #[cfg(not(target_family = "wasm"))]
    /// # {
    /// use tokio::runtime;
    ///
    /// let rt = runtime::Builder::new_multi_thread()
    ///   .enable_alt_timer()
    ///   .build()
    ///   .unwrap();
    /// # }
    /// ```
    #[cfg(all(tokio_unstable, feature = "time", feature = "rt-multi-thread"))]
    #[cfg_attr(
        docsrs,
        doc(cfg(all(tokio_unstable, feature = "time", feature = "rt-multi-thread")))
    )]
    pub fn enable_alt_timer(&mut self) -> &mut Self {
        self.enable_time();
        self.timer_flavor = TimerFlavor::Alternative;
        self
    }

    /// Enable eager hand-off of the I/O and time drivers for multi-threaded
    /// runtimes, which is disabled by default.
    ///
    /// When this option is enabled, a worker thread which has parked on the I/O
    /// or time driver will notify another worker thread once it is preparing to
    /// begin polling a task from the run queue, so that the notified worker can
    /// begin polling the I/O or time driver. This can reduce the latency with
    /// which I/O and timer notifications are processed, especially when some
    /// tasks have polls that take a long time to complete. In addition, it can
    /// reduce the risk of a deadlock which may occur when a task blocks the
    /// worker thread which is holding the I/O or time driver until some other
    /// task, which is waiting for a notification from *that* driver, unblocks
    /// it.
    ///
    /// This option is disabled by default, as enabling it may potentially
    /// increase contention due to extra synchronization in cross-driver
    /// wakeups.
    ///
    /// This option only applies to multi-threaded runtimes. Attempting to use
    /// this option with any other runtime type will have no effect.
    ///
    /// **Note**: This is an [unstable API][unstable]. Eager driver hand-off is
    /// an experimental feature whose behavior may be removed or changed in 1.x
    /// releases. See [the documentation on unstable features][unstable] for
    /// details.
    ///
    /// [unstable]: crate#unstable-features
    #[cfg(all(tokio_unstable, feature = "rt-multi-thread"))]
    #[cfg_attr(docsrs, doc(cfg(all(tokio_unstable, feature = "rt-multi-thread"))))]
    pub fn enable_eager_driver_handoff(&mut self) -> &mut Self {
        self.enable_eager_driver_handoff = true;
        self
    }

    /// Sets the number of worker threads the `Runtime` will use.
    ///
    /// This can be any number above 0 though it is advised to keep this value
    /// on the smaller side.
    ///
    /// This will override the value read from environment variable `TOKIO_WORKER_THREADS`.
    ///
    /// # Default
    ///
    /// The default value is the number of cores available to the system.
    ///
    /// When using the `current_thread` runtime this method has no effect.
    ///
    /// # Examples
    ///
    /// ## Multi threaded runtime with 4 threads
    ///
    /// ```
    /// # #[cfg(not(target_family = "wasm"))]
    /// # {
    /// use tokio::runtime;
    ///
    /// // This will spawn a work-stealing runtime with 4 worker threads.
    /// let rt = runtime::Builder::new_multi_thread()
    ///     .worker_threads(4)
    ///     .build()
    ///     .unwrap();
    ///
    /// rt.spawn(async move {});
    /// # }
    /// ```
    ///
    /// ## Current thread runtime (will only run on the current thread via `Runtime::block_on`)
    ///
    /// ```
    /// use tokio::runtime;
    ///
    /// // Create a runtime that _must_ be driven from a call
    /// // to `Runtime::block_on`.
    /// let rt = runtime::Builder::new_current_thread()
    ///     .build()
    ///     .unwrap();
    ///
