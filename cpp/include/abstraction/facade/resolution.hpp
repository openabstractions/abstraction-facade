#pragma once
#include <abstraction/facade/rec.h>
#include <abstraction/ipc/frame.hpp>
#include <algorithm>
#include <cstdlib>
#ifdef _WIN32
#include <abstraction/ipc/process.hpp>
#endif

namespace abstraction::facade {

inline std::string runtime_endpoint() {
    if (const char* value = std::getenv("ABSTRACTION_RUNTIME_ENDPOINT")) {
        if (*value) return value;
    }
#ifdef _WIN32
    return std::string(R"(\\.\pipe\openabstractions-user-)") + ipc::process_user_sid() + "-runtime-v1";
#else
    if (const char* value = std::getenv("XDG_RUNTIME_DIR")) {
        if (*value) return std::string(value) + "/openabstractions-runtime-v1.sock";
    }
    const char* temporary = std::getenv("TMPDIR");
    const char* user = std::getenv("USER");
    return std::string(temporary && *temporary ? temporary : "/tmp") +
        "/openabstractions-runtime-v1-" + (user ? user : "") + ".sock";
#endif
}

class ResolutionError : public std::runtime_error {
public:
    std::string status;
    explicit ResolutionError(std::string value)
        : std::runtime_error("resolution: " + value), status(std::move(value)) {}
};

namespace resolution_detail {
inline bool contains(const std::vector<std::string>& values, const std::string& value) {
    return std::find(values.begin(), values.end(), value) != values.end();
}
inline bool distinct(const std::vector<std::string>& values) {
    for (std::size_t i = 0; i < values.size(); ++i) {
        if (values[i].empty() ||
            std::find(values.begin(), values.begin() + i, values[i]) != values.begin() + i)
            return false;
    }
    return true;
}
inline bool satisfies(const std::vector<std::string>& available,
                      const std::vector<std::string>& required) {
    for (const auto& value : required) if (!contains(available, value)) return false;
    return true;
}
}

inline void validate_resolve_request(const ResolveRequest& request) {
    if (request.capability.empty() || request.contracts.empty() ||
        !resolution_detail::distinct(request.contracts) ||
        !resolution_detail::distinct(request.guarantees) ||
        (request.scope != "any" && request.scope != "local" && request.scope != "remote"))
        throw ResolutionError("invalid_request");
}

// Generated codecs check wire shape. Cross-field selection meaning is checked
// before a reference can supply an endpoint to a capability client.
inline void validate_resolve_result(const ResolveRequest& request, const ResolveResult& result) {
    validate_resolve_request(request);
    if (!resolution_detail::contains(kResolutionStatusNames, result.status))
        throw ResolutionError("invalid_resolution");
    if (result.status != "resolved") {
        if (result.reference) throw ResolutionError("invalid_resolution");
        return;
    }
    if (!result.reference) throw ResolutionError("invalid_resolution");
    const auto& ref = *result.reference;
    if (ref.provider.empty() || ref.capability != request.capability || ref.contract.empty() ||
        !resolution_detail::contains(request.contracts, ref.contract) ||
        (ref.scope != "local" && ref.scope != "remote") ||
        (request.scope != "any" && request.scope != ref.scope) ||
        ref.transport.empty() || ref.endpoint.empty() || ref.endpoint.find('\0') != std::string::npos ||
        !resolution_detail::distinct(ref.guarantees) ||
        !resolution_detail::satisfies(ref.guarantees, request.guarantees))
        throw ResolutionError("invalid_resolution");
}

inline ServiceReference local_binding(const ResolveRequest& request, const ResolveResult& result) {
    validate_resolve_result(request, result);
    if (result.status != "resolved") throw ResolutionError(result.status);
    const auto& ref = *result.reference;
    if (ref.scope != "local" || ref.transport != "oa-framed-local@1")
        throw ResolutionError("unsupported_transport");
    return ref;
}

// Shared FrameTransport performs the call; resolution neither activates a
// provider nor proves server trust. Transport failures propagate unchanged.
class ResolutionClient {
public:
    explicit ResolutionClient(std::string endpoint = runtime_endpoint(),
                              std::uint32_t timeout_ms = 5000)
        : endpoint_(std::move(endpoint)), timeout_(timeout_ms) {}

    ResolveResult Resolve(const ResolveRequest& request) const {
        validate_resolve_request(request);
        ipc::FrameTransport transport(endpoint_, timeout_, 1 << 20);
        ResolverClient<ipc::FrameTransport> client(transport);
        auto result = client.Resolve(request);
        validate_resolve_result(request, result);
        return result;
    }

private:
    std::string endpoint_;
    std::uint32_t timeout_;
};
}
