// Included after the existing private named-pipe fixtures on Windows.
template<class F> void expect_cancelled(F call) {
    try {call();} catch(const abstraction::ipc::FrameError& e) {
        require(e.status==abstraction::ipc::Status::cancelled);return;
    }
    throw std::runtime_error("expected cancelled waiting scope");
}
void binding_cancellation() {
    namespace ipc=abstraction::ipc;
    ipc::CancellationSource cancelled;cancelled.Cancel();
    expect_cancelled([&]{f::Machine("unused").WithCancellation(cancelled.Token()).ResolveJobs();});
    auto cancelledMachine=f::Machine("unused").WithCancellation(cancelled.Token());
    expect_cancelled([&]{cancelledMachine.Log();});
    expect_cancelled([&]{cancelledMachine.Config();});
    expect_cancelled([&]{cancelledMachine.Router();});
    auto original=f::JobsClient("unused");
    const auto future=ipc::Clock::now()+std::chrono::seconds(5);
    expect_cancelled([&]{original.WithCancellation(cancelled.Token()).WithDeadline(future).GetHistoryWindow();});
    expect_cancelled([&]{original.WithDeadline(future).WithCancellation(cancelled.Token()).GetHistoryWindow();});
    ipc::CancellationSource live;
    const auto expired=ipc::Clock::now()-std::chrono::seconds(1);
    timeout([&]{original.WithCancellation(live.Token()).WithDeadline(expired).GetHistoryWindow();});
    timeout([&]{original.WithDeadline(expired).WithCancellation(live.Token()).GetHistoryWindow();});
    {
        JobFixture handler;f::job_api::RecoverableAcceptanceDispatcher dispatcher(handler);
        SingleFrame selected([&](const std::string& frame){require(f::job_api::service_payload(frame).method=="GetHistoryWindow");return dispatcher.ExchangeFrame(frame);});
        f::JobsClient ordinary(selected.endpoint);
        expect_cancelled([&]{ordinary.WithCancellation(cancelled.Token()).GetHistoryWindow();});
        require(ordinary.GetHistoryWindow().logical_owner=="owner");selected.finish();
    }
    for(const std::string cap:{"logging","config","router","job","operations"}) {
        std::atomic<unsigned> provider_calls{0},resolutions{0};
        SingleFrame selected([&](const std::string& frame){
            ++provider_calls;
            if(cap=="logging") {auto v=abstraction::logging::service_payload(frame);require(v.method=="Write");return std::string{};}
            if(cap=="config") {auto v=abstraction::config::service_payload(frame);require(v.method=="Read");abstraction::config::OAConfigReaderReadResult p;p.value.stamp="fresh";std::string raw;abstraction::config::enc_oaconfigreaderreadresult(raw,p,1);return abstraction::config::service_reply(v,raw,nullptr);}
            if(cap=="router") {auto v=abstraction::router::service_payload(frame);require(v.method=="Hosts");abstraction::router::OARouterHostsResult p;std::string raw;abstraction::router::enc_oarouterhostsresult(raw,p,1);return abstraction::router::service_reply(v,raw,nullptr);}
            if(cap=="operations") {auto v=f::job_api::service_payload(frame);require(v.method=="ObserveWork");f::job_api::OAOperationControlObserveWorkResult p;p.value.outcome="unknown";std::string raw;f::job_api::enc_oaoperationcontrolobserveworkresult(raw,p,1);return f::job_api::service_reply(v,raw,nullptr);}
            auto v=f::job_api::service_payload(frame);require(v.method=="GetHistoryWindow");JobFixture handler;f::job_api::RecoverableAcceptanceDispatcher dispatch(handler);return dispatch.ExchangeFrame(frame);
        });
        ResolverFixture provider;provider.capability="abstraction."+(cap=="operations"?std::string("job"):cap);provider.endpoint=selected.endpoint;
        provider.contract=provider.capability+(cap=="logging"?"/sink@1":cap=="config"?"/reader@1":cap=="router"?"/router@1":cap=="job"?"/acceptance@1":"/operations@1");
        f::ResolverDispatcher dispatcher(provider);
        SingleFrame bootstrap([&](const std::string& frame){++resolutions;return dispatcher.ExchangeFrame(frame);},2);
        const f::Machine ordinary(bootstrap.endpoint);
        ipc::CancellationSource source;
        const auto scoped=ordinary.WithCancellation(source.Token());
        auto bind=[&](const f::Machine& machine)->std::function<void()> {
            if(cap=="logging"){auto c=machine.ResolveLog({"required@1"},"local",future);return [c]()mutable{c.Log(1,"fresh call");};}
            if(cap=="config"){auto c=machine.ResolveConfig({"required@1"},"local",future);return [c]{c.ReadWithOverrides({});};}
            if(cap=="router"){auto c=machine.ResolveRouter({"required@1"},"local",future);return [c]{c.Hosts();};}
            if(cap=="operations"){auto c=machine.ResolveJobOperations({"required@1"},"local",future);return [c]()mutable{c.ObserveWork({"key","epoch"});};}
            auto c=machine.ResolveJobs({"required@1"},"local",future);return [c]()mutable{c.GetHistoryWindow();};
        };
        auto call=bind(scoped);
        source.Cancel();expect_cancelled(call);
        require(provider_calls==0&&resolutions==1); // No provider I/O or implicit resolution after cancellation.
        bind(ordinary)(); // Original immutable Machine remains usable for a fresh call.
        bootstrap.finish();selected.finish();require(provider_calls==1&&resolutions==2);
    }
}
