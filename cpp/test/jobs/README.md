# Outside job consumer

The existing no-argument checks and `--runtime ENDPOINT --jobs KEY` admission
mode remain available. Configure with `-DOA_TEST_DOWNLOAD_REQUEST=ON` to add
`--execute ENDPOINT HTTP_URL KEY`. This mode uses generated request vocabulary,
resolves job operations, submits under caller-owned identity, observes typed
completion and copies the result. It emits `RESULT ` followed by hexadecimal
bytes for the fixture to compare. The total client waiting budget is 15 seconds.

Install `abstraction_facade` in jobs-only mode, `abstraction_job_acceptance`,
`abstraction_ipc` and `abstraction_download_request` into an isolated prefix.
Configure this directory with that CMAKE_PREFIX_PATH. The consumer refuses a
configuration that imports unrelated capability clients or embedded provider
targets. No source include directory is accepted as a workaround.

Run the actual service fixture from facade/go:

    OA_CPP_EXECUTION_PROBE=/absolute/path/to/facade_jobs_consumer go test -v -run TestCppDurableHTTPExecution ./runtime

The Go fixture owns its private runtime/store and local HTTP server. Application
arguments contain only endpoint, URL and key. The source body crosses multiple
64 KiB result chunks and includes every byte value. This test proves service-owned
HTTP execution and typed result consumption; native adapters, crash recovery,
installation and remote trust are separate evidence.
