// Every resolution that yields no service throws facade::ResolutionError with its
// context; the caller's cancellation stays ipc::FrameError. Installed-runtime
// selection is replaced by a shim so the cases hold on any machine.
#include <abstraction/ipc/client.h>
#include <cstdint>
static oa_ipc_status selection_status = OA_IPC_UNTRUSTED;
static unsigned selections;
static oa_ipc_status shim_select(uint32_t, oa_ipc_cancellation*, oa_ipc_runtime_selection** out) {
    ++selections;
    *out = nullptr;
    return selection_status;
}
#define oa_ipc_select_runtime shim_select
#include <abstraction/facade/client.hpp>
#undef oa_ipc_select_runtime
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <atomic>
#include <functional>
#include <iostream>
#include <thread>

namespace f = abstraction::facade;
namespace ipc = abstraction::ipc;
static void require(bool value, const char* what) { if (!value) throw std::runtime_error(what); }

static f::ResolutionError resolution_error(const std::function<void()>& call) {
    try { call(); } catch (const f::ResolutionError& e) { return e; }
    throw std::runtime_error("expected facade::ResolutionError");
}

// One named pipe that answers each frame with the generated resolver dispatcher.
class ResolverPipe {
    HANDLE pipe_;
    std::thread worker_;
    static void io(HANDLE p, void* bytes, DWORD count, bool write) {
        auto* b = static_cast<char*>(bytes);
        while (count) {
            DWORD n = 0;
            BOOL ok = write ? WriteFile(p, b, count, &n, nullptr) : ReadFile(p, b, count, &n, nullptr);
            if (!ok || !n) throw std::runtime_error("fixture pipe I/O failed");
            b += n; count -= n;
        }
    }
public:
    std::string endpoint;
    ResolverPipe(f::Resolver& handler, unsigned exchanges) {
        static std::atomic<unsigned> serial{0};
        endpoint = R"(\\.\pipe\oa-resolution-error-)" + std::to_string(GetCurrentProcessId()) + "-" + std::to_string(++serial);
        pipe_ = CreateNamedPipeA(endpoint.c_str(), PIPE_ACCESS_DUPLEX, PIPE_TYPE_BYTE | PIPE_WAIT, 1, 1 << 20, 1 << 20, 0, nullptr);
        require(pipe_ != INVALID_HANDLE_VALUE, "fixture pipe");
        worker_ = std::thread([this, &handler, exchanges] {
            try {
                f::ResolverDispatcher dispatcher(handler);
                for (unsigned i = 0; i < exchanges; ++i) {
                    if (!ConnectNamedPipe(pipe_, nullptr) && GetLastError() != ERROR_PIPE_CONNECTED) return;
                    unsigned char h[4]; io(pipe_, h, 4, false);
                    const unsigned n = (unsigned(h[0]) << 24) | (unsigned(h[1]) << 16) | (unsigned(h[2]) << 8) | h[3];
                    std::string frame(n, '\0'); io(pipe_, frame.data(), n, false);
                    auto reply = dispatcher.exchange_frame(frame);
                    const unsigned r = static_cast<unsigned>(reply.size());
                    unsigned char rh[4] = {static_cast<unsigned char>(r >> 24), static_cast<unsigned char>(r >> 16), static_cast<unsigned char>(r >> 8), static_cast<unsigned char>(r)};
                    io(pipe_, rh, 4, true); io(pipe_, reply.data(), r, true); FlushFileBuffers(pipe_);
                    DisconnectNamedPipe(pipe_);
                }
            } catch (...) { DisconnectNamedPipe(pipe_); }
        });
    }
    ~ResolverPipe() { worker_.join(); CloseHandle(pipe_); }
};

struct Catalogue : f::Resolver {
    bool registered = false;
    std::string provider_endpoint = R"(\\.\pipe\oa-resolution-error-provider)";
    f::ResolveResult resolve(const f::ResolveRequest& request) override {
        f::ResolveResult result;
        if (!registered) { result.status = f::ResolutionStatus::Unavailable; return result; }
        result.status = f::ResolutionStatus::Resolved;
        f::ServiceReference ref;
        ref.provider = "fixture"; ref.capability = request.capability; ref.contract = request.contracts.front();
        ref.scope = f::Scope::Local; ref.transport = "oa-framed-local@1"; ref.endpoint = provider_endpoint;
        result.reference = ref;
        return result;
    }
};

// VISION.md 2026-09-16: an adopted capability with no runtime fails with the facade's
// resolution error, visibly, and the application substitutes nothing. "A logging seam
// that quietly writes to stderr instead is the same defect" [LOG-S11]. resolve_log
// throws the error and returns no Logger to write through.
static void no_runtime_installed() {
    selection_status = OA_IPC_UNTRUSTED;
    auto e = resolution_error([] { f::Machine{}.resolve_log(); });
    require(e.status == f::kRuntimeUnavailable, "status");
    require(e.capability == "abstraction.logging" && e.contract == "abstraction.logging/sink@1", "capability and contract");
    require(e.looked_for == "the installed runtime", "looked for");
    require(!e.refusal(), "runtime_unavailable is no resolver refusal");
    bool untrusted = false;
    try { e.rethrow_cause(); } catch (const ipc::FrameError& cause) { untrusted = cause.status == ipc::Status::Untrusted; }
    require(untrusted, "selection failure is the cause");
    require(std::string(e.what()) == "service resolution: runtime_unavailable: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime", "message");
    std::cout << "PASS no runtime installed: runtime_unavailable at the installed runtime, cause FrameError untrusted\n";
}

static void explicit_endpoint_nobody_listens_on() {
    const std::string absent = R"(\\.\pipe\oa-resolution-error-absent-)" + std::to_string(GetCurrentProcessId());
    auto e = resolution_error([&] { f::Machine(absent).resolve_config(); });
    require(e.status == f::kRuntimeUnavailable && e.contract == "abstraction.config/reader@1", "status and contract");
    require(e.looked_for == "the explicit endpoint " + absent, "looked for");
    bool transport = false;
    try { e.rethrow_cause(); } catch (const ipc::FrameError&) { transport = true; }
    require(transport, "transport failure is the cause");
    std::cout << "PASS explicit endpoint nobody listens on: runtime_unavailable at the explicit endpoint, cause FrameError\n";
}

static void runtime_without_the_service_then_success() {
    Catalogue catalogue;
    ResolverPipe resolver(catalogue, 2);
    auto e = resolution_error([&] { f::Machine(resolver.endpoint).resolve_log(); });
    require(e.status == "unavailable" && e.refusal() == f::ResolutionStatus::Unavailable, "resolver refusal");
    require(e.looked_for == "the explicit endpoint " + resolver.endpoint && !e.cause, "context without cause");
    catalogue.registered = true;
    auto logger = f::Machine(resolver.endpoint).resolve_log();
    (void)logger;
    std::cout << "PASS runtime without the service: unavailable, no cause; success unchanged\n";
}

static void caller_cancellation_stays_a_transport_outcome() {
    selection_status = OA_IPC_CANCELLED;
    bool cancelled = false;
    try { f::Machine{}.resolve_router(); }
    catch (const f::ResolutionError&) { throw std::runtime_error("cancellation became a ResolutionError"); }
    catch (const ipc::FrameError& e) { cancelled = e.status == ipc::Status::Cancelled; }
    require(cancelled, "cancellation lost");
    selection_status = OA_IPC_UNTRUSTED;
    std::cout << "PASS caller cancellation stays FrameError cancelled\n";
}

int main() {
    _putenv_s("ABSTRACTION_RUNTIME_ENDPOINT", R"(\\.\pipe\oa-resolution-error-installed)");
    try {
        no_runtime_installed();
        explicit_endpoint_nobody_listens_on();
        runtime_without_the_service_then_success();
        caller_cancellation_stays_a_transport_outcome();
        require(selections == 2, "selection shim reached");
    } catch (const std::exception& e) {
        std::cerr << "FAIL " << e.what() << "\n";
        return 1;
    }
}
