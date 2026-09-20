// On a platform the runtime declares unsupported, resolving through the installed
// runtime throws runtime_unavailable naming the platform, before any selection.
// The build names Android as its target; selection is a shim that counts calls.
#define ABSTRACTION_FACADE_TARGET_PLATFORM "android"
#include <abstraction/ipc/client.h>
#include <cstdint>
static unsigned selections;
static oa_ipc_status shim_select(uint32_t, oa_ipc_cancellation*, oa_ipc_runtime_selection** out) {
    ++selections;
    *out = nullptr;
    return OA_IPC_UNTRUSTED;
}
#define oa_ipc_select_runtime shim_select
#include <abstraction/facade/client.hpp>
#undef oa_ipc_select_runtime
#include <iostream>
#include <stdexcept>
#include <string>

namespace f = abstraction::facade;
static void require(bool value, const char* what) { if (!value) throw std::runtime_error(what); }

int main() {
    try {
        require(f::unsupported_platform() == "android", "declared platform");
        bool refused = false;
        try {
            f::Machine{}.resolve_log();
        } catch (const f::ResolutionError& e) {
            refused = true;
            require(e.status == f::kRuntimeUnavailable, "status");
            require(e.platform == "android", "platform");
            require(e.capability == "abstraction.logging" && e.contract == "abstraction.logging/sink@1", "capability and contract");
            require(e.looked_for == "the installed runtime", "looked for");
            require(!e.cause && !e.refusal(), "no cause and no resolver refusal");
            require(std::string(e.what()) == "service resolution: runtime_unavailable: abstraction.logging/sink@1 "
                                              "(capability abstraction.logging) at the installed runtime: "
                                              "no supported OpenAbstractions runtime exists for android", "message");
        }
        require(refused, "resolution on an unsupported platform succeeded");
        require(selections == 0, "selection reached on an unsupported platform");
        std::cout << "PASS unsupported platform: runtime_unavailable names android before selection\n";
    } catch (const std::exception& e) {
        std::cerr << "FAIL " << e.what() << "\n";
        return 1;
    }
}
