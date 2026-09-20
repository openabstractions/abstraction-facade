// Included after the existing private named-pipe fixtures on Windows.
template<class F> void expect_cancelled(F call) {
    try {call();} catch(const abstraction::ipc::FrameError& e) {
        require(e.status==abstraction::ipc::Status::Cancelled);return;
    }
    throw std::runtime_error("expected cancelled waiting scope");
}
void binding_cancellation() {
    namespace ipc=abstraction::ipc;
    ipc::CancellationSource cancelled;cancelled.cancel();
    expect_cancelled([&]{f::Machine("unused").with_cancellation(cancelled.token()).resolve_jobs();});
    auto cancelledMachine=f::Machine("unused").with_cancellation(cancelled.token());
    expect_cancelled([&]{cancelledMachine.log();});
    expect_cancelled([&]{cancelledMachine.config();});
    expect_cancelled([&]{cancelledMachine.router();});
    auto original=f::JobsClient("unused");
    const auto future=ipc::Clock::now()+std::chrono::seconds(5);
    expect_cancelled([&]{original.with_cancellation(cancelled.token()).with_deadline(future).get_history_window();});
    expect_cancelled([&]{original.with_deadline(future).with_cancellation(cancelled.token()).get_history_window();});
    ipc::CancellationSource live;
    const auto expired=ipc::Clock::now()-std::chrono::seconds(1);
    timeout([&]{original.with_cancellation(live.token()).with_deadline(expired).get_history_window();});
    timeout([&]{original.with_deadline(expired).with_cancellation(live.token()).get_history_window();});
    {
        JobFixture handler;f::job_api::RecoverableAcceptanceDispatcher dispatcher(handler);
        SingleFrame selected([&](const std::string& frame){require(f::job_api::detail::service_payload(frame).method=="GetHistoryWindow");return dispatcher.exchange_frame(frame);});
        f::JobsClient ordinary(selected.endpoint);
        expect_cancelled([&]{ordinary.with_cancellation(cancelled.token()).get_history_window();});
        require(ordinary.get_history_window().logical_owner=="owner");selected.finish();
    }
    for(const std::string cap:{"logging","config","router","job","operations"}) {
        std::atomic<unsigned> provider_calls{0},resolutions{0};
        SingleFrame selected([&](const std::string& frame){
            ++provider_calls;
            if(cap=="logging") {auto v=abstraction::logging::detail::service_payload(frame);require(v.method=="Write");return std::string{};}
            if(cap=="config") {auto v=abstraction::config::detail::service_payload(frame);require(v.method=="Read");abstraction::config::detail::OAConfigReaderReadResult p;p.value.stamp="fresh";std::string raw;abstraction::config::detail::enc_oa_config_reader_read_result(raw,p,1);return abstraction::config::detail::service_reply(v,raw,nullptr);}
            if(cap=="router") {auto v=abstraction::router::detail::service_payload(frame);require(v.method=="Hosts");abstraction::router::detail::OARouterHostsResult p;std::string raw;abstraction::router::detail::enc_oa_router_hosts_result(raw,p,1);return abstraction::router::detail::service_reply(v,raw,nullptr);}
            if(cap=="operations") {auto v=f::job_api::detail::service_payload(frame);require(v.method=="ObserveWork");f::job_api::detail::OAOperationControlObserveWorkResult p;p.value.outcome=abstraction::job::acceptance::ObservationOutcome::Unknown;std::string raw;f::job_api::detail::enc_oa_operation_control_observe_work_result(raw,p,1);return f::job_api::detail::service_reply(v,raw,nullptr);}
            auto v=f::job_api::detail::service_payload(frame);require(v.method=="GetHistoryWindow");JobFixture handler;f::job_api::RecoverableAcceptanceDispatcher dispatch(handler);return dispatch.exchange_frame(frame);
        });
        ResolverFixture provider;provider.capability="abstraction."+(cap=="operations"?std::string("job"):cap);provider.endpoint=selected.endpoint;
        provider.contract=provider.capability+(cap=="logging"?"/sink@1":cap=="config"?"/reader@1":cap=="router"?"/router@1":cap=="job"?"/acceptance@1":"/operations@1");
        f::ResolverDispatcher dispatcher(provider);
        SingleFrame bootstrap([&](const std::string& frame){++resolutions;return dispatcher.exchange_frame(frame);},2);
        const f::Machine ordinary(bootstrap.endpoint);
        ipc::CancellationSource source;
        const auto scoped=ordinary.with_cancellation(source.token());
        auto bind=[&](const f::Machine& machine)->std::function<void()> {
            if(cap=="logging"){auto c=machine.resolve_log({"required@1"},abstraction::facade::Scope::Local,future);return [c]()mutable{c.log(1,"fresh call");};}
            if(cap=="config"){auto c=machine.resolve_config({"required@1"},abstraction::facade::Scope::Local,future);return [c]{c.read_with_overrides({});};}
            if(cap=="router"){auto c=machine.resolve_router({"required@1"},abstraction::facade::Scope::Local,future);return [c]{c.hosts();};}
            if(cap=="operations"){auto c=machine.resolve_job_operations({"required@1"},abstraction::facade::Scope::Local,future);return [c]()mutable{c.observe_work({"key","epoch"});};}
            auto c=machine.resolve_jobs({"required@1"},abstraction::facade::Scope::Local,future);return [c]()mutable{c.get_history_window();};
        };
        auto call=bind(scoped);
        source.cancel();expect_cancelled(call);
        require(provider_calls==0&&resolutions==1); // No provider I/O or implicit resolution after cancellation.
        bind(ordinary)(); // Original immutable Machine remains usable for a fresh call.
        bootstrap.finish();selected.finish();require(provider_calls==1&&resolutions==2);
    }
}
