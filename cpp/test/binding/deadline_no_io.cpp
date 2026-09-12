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
        if(e.status==abstraction::ipc::Status::timeout && opens==0)return;
        throw std::runtime_error("expired budget opened transport or lost timeout type");
    }
    throw std::runtime_error("expired budget was accepted");
}
int main() {
    const auto deadline=abstraction::ipc::Clock::now()-std::chrono::seconds(1);
    abstraction::facade::Machine machine("missing-endpoint");
    expired([&]{machine.ResolveLog({},"any",deadline);});
    expired([&]{machine.ResolveConfig({},"any",deadline);});
    abstraction::logging::Logger log("missing-endpoint",deadline);auto copy=log;
    expired([&]{copy.Log(1,"must not send");});
    abstraction::config::Client config("missing-endpoint",deadline);
    expired([&]{config.ReadWithOverrides({});});
    abstraction::ipc::FrameTransport frame("missing-endpoint",deadline);
    expired([&]{frame.ExchangeFrame("{}");});
    expired([&]{frame.WriteFrame("{}");});
}
