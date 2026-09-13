// Included after private named-pipe fixtures; observes resolver answers only.
struct StatusResolver : f::Resolver {
    std::vector<std::string> states;
    unsigned next = 0;
    f::ResolveResult Resolve(const f::ResolveRequest& request) override {
        require(next < states.size());
        const auto state = states[next++];
        if (state == "resolved") return resolved(request, "explicit-selected-provider");
        f::ResolveResult result;
        result.status = state;
        return result;
    }
};
void status_observation() {
    auto requests = f::DefaultStatusRequests();
    require(requests.size() == 5);
    require(requests[4].contracts == std::vector<std::string>{"abstraction.config/editor@1"});
    for (const auto& q : requests) require(q.scope == "local" && q.guarantees.empty());
    StatusResolver provider;
    provider.states = {"resolved", "not_ready", "forbidden", "unmet_requirements", "unavailable"};
    f::ResolverDispatcher dispatcher(provider);
    SingleFrame bootstrap([&](const std::string& frame) { return dispatcher.ExchangeFrame(frame); }, 5);
    auto observed = f::Machine(bootstrap.endpoint).Observe();
    bootstrap.finish();
    require(!observed.error && observed.observation.bootstrap.state == "unknown");
    require(observed.observation.capabilities.size() == 5);
    for (unsigned i = 0; i != 5; ++i) {
        const auto& capability = observed.observation.capabilities[i];
        require(capability.result && capability.result->status == provider.states[i]);
        require(capability.request.contracts == requests[i].contracts);
    }
    require(observed.observation.capabilities[0].result->reference->endpoint == "explicit-selected-provider");

    f::BootstrapObservation running;
    running.state = "running";
    running.detail = "independent supervisor observation";
    StatusResolver pending;
    pending.states = {"not_ready"};
    f::ResolverDispatcher pendingDispatcher(pending);
    std::string stoppedEndpoint;
    {
        SingleFrame live([&](const std::string& frame) { return pendingDispatcher.ExchangeFrame(frame); });
        stoppedEndpoint = live.endpoint;
        auto result = f::Machine(live.endpoint).Observe({requests[0]}, running);
        live.finish();
        require(!result.error && result.observation.bootstrap.state == "running");
        require(result.observation.capabilities[0].result->status == "not_ready");
    }
    // Closed resolver transport gives no new installation or capability fact.
    auto stopped = f::Machine(stoppedEndpoint).Observe(requests, running,
        abstraction::ipc::Clock::now() + std::chrono::milliseconds(100));
    require(stopped.error && stopped.observation.bootstrap.state == "running");
    for (const auto& capability : stopped.observation.capabilities) require(!capability.result);

    unsigned calls = 0;
    SingleFrame partial([&](const std::string& frame) {
        if (++calls == 2) return std::string{}; // peer closes without a second reply
        auto envelope = f::service_payload(frame);
        f::OAResolverResolveResult payload;
        payload.value.status = "forbidden";
        std::string raw;
        f::enc_oaresolverresolveresult(raw, payload, 1);
        return f::service_reply(envelope, raw, nullptr);
    }, 2);
    auto incomplete = f::Machine(partial.endpoint).Observe(requests);
    partial.finish();
    require(incomplete.error && incomplete.observation.capabilities.size() == 5);
    require(incomplete.observation.capabilities[0].result->status == "forbidden");
    for (unsigned i = 1; i != 5; ++i) require(!incomplete.observation.capabilities[i].result);
    for (const std::string state : {"", "ready", "missing"}) {
        auto invalid = f::UnknownBootstrap();
        invalid.state = state;
        auto result = f::Machine("unused").Observe(requests, invalid);
        require(static_cast<bool>(result.error));
        try {
            std::rethrow_exception(result.error);
        } catch (const std::invalid_argument&) {
            continue;
        }
        throw std::runtime_error("invalid bootstrap was not refused before I/O");
    }
    abstraction::ipc::CancellationSource cancellation;
    cancellation.Cancel();
    auto cancelled = f::Machine("unused").WithCancellation(cancellation.Token()).Observe();
    require(cancelled.error && cancelled.observation.bootstrap.state == "unknown");
    expect_cancelled([&] { std::rethrow_exception(cancelled.error); });
    auto expired = f::Machine("unused").Observe(requests, f::UnknownBootstrap(),
        abstraction::ipc::Clock::now() - std::chrono::seconds(1));
    require(static_cast<bool>(expired.error));
    timeout([&] { std::rethrow_exception(expired.error); });
}
