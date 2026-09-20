#include <abstraction/ipc/client.h>
#include <stdexcept>
static unsigned opens;
static oa_ipc_status tracked_open(const char* path, size_t size, uint32_t timeout, oa_ipc_connection** connection) {
    ++opens;
    return oa_ipc_open(path,size,timeout,connection);
}
#define oa_ipc_open tracked_open
#include <abstraction/facade/client.hpp>
#undef oa_ipc_open

template<class Fn> void expired(Fn fn) {
    try { fn(); } catch(const abstraction::ipc::FrameError& e) {
        if(e.status==abstraction::ipc::Status::Timeout && opens==0)return;
        throw std::runtime_error("expired budget opened transport or lost timeout type");
    }
    throw std::runtime_error("expired budget was accepted");
}
int main() {
    const auto deadline=abstraction::ipc::Clock::now()-std::chrono::seconds(1);
    abstraction::facade::Machine machine("missing-endpoint");
    expired([&]{machine.resolve_log({},abstraction::facade::Scope::Any,deadline);});
    expired([&]{machine.resolve_config({},abstraction::facade::Scope::Any,deadline);});
    abstraction::logging::Logger log("missing-endpoint",deadline);auto copy=log;
    expired([&]{copy.log(1,"must not send");});
    abstraction::config::Client config("missing-endpoint",deadline);
    expired([&]{config.read_with_overrides({});});
    expired([&]{machine.resolve_router({},abstraction::facade::Scope::Any,deadline);});
    abstraction::router::Client router("missing-endpoint",deadline);auto routerCopy=router;
    expired([&]{routerCopy.models();});
    expired([&]{routerCopy.hosts();});
    expired([&]{routerCopy.pick({});});
    expired([&]{machine.resolve_jobs({},abstraction::facade::Scope::Any,deadline);});
    abstraction::facade::JobsClient jobs("missing-endpoint",deadline);
    abstraction::facade::job_api::Submission submission;
    submission.identity.key="key";submission.identity.history_epoch="epoch";submission.kind="test";
    expired([&]{jobs.get_history_window();});
    expired([&]{jobs.submit(submission);});
    expired([&]{jobs.reconcile(submission.identity);});
    expired([&]{jobs.cancel_work(submission.identity);});
    auto jobCopy=jobs.with_deadline(deadline);
    expired([&]{jobCopy.reconcile(submission.identity);});
    abstraction::ipc::FrameTransport frame("missing-endpoint",deadline);
    expired([&]{frame.exchange_frame("{}");});
    expired([&]{frame.write_frame("{}");});
}
