// resolve_resource_table/resolve_resource_leases bind through the same
// ResolutionClient path as resolve_rights. An explicit endpoint nobody listens
// on needs no fixture server: resolution fails runtime_unavailable, naming the
// capability and contract this accessor asked for, exactly as
// abstraction-facade/cpp/test/binding/resolution_error.cpp proves for
// resolve_config.
#include <abstraction/facade/resource.hpp>
#include <functional>
#include <iostream>
#include <stdexcept>
#include <string>

namespace f = abstraction::facade;
static void require(bool value, const char* what) { if (!value) throw std::runtime_error(what); }

static f::ResolutionError resolution_error(const std::function<void()>& call) {
    try { call(); } catch (const f::ResolutionError& e) { return e; }
    throw std::runtime_error("expected facade::ResolutionError");
}

int main() {
    try {
        const std::string absent =
#ifdef _WIN32
            R"(\\.\pipe\oa-resource-facade-test-absent)";
#else
            "/tmp/oa-resource-facade-test-absent.sock";
#endif
        f::ResolutionClient resolver(absent, 500);

        auto table_error = resolution_error([&] { f::resolve_resource_table(resolver); });
        require(table_error.status == std::string(f::kRuntimeUnavailable), "table status");
        require(table_error.capability == "abstraction.resource", "table capability");
        require(table_error.contract == "abstraction.resource/table@1", "table contract");
        require(table_error.looked_for == "the explicit endpoint " + absent, "table looked_for");

        auto leases_error = resolution_error([&] { f::resolve_resource_leases(resolver); });
        require(leases_error.status == std::string(f::kRuntimeUnavailable), "leases status");
        require(leases_error.capability == "abstraction.resource", "leases capability");
        require(leases_error.contract == "abstraction.resource/leases@1", "leases contract");
        require(leases_error.looked_for == "the explicit endpoint " + absent, "leases looked_for");

        std::cout << "PASS resolve_resource_table/resolve_resource_leases name their capability and contract on refusal\n";
        return 0;
    } catch (const std::exception& e) {
        std::cerr << "FAIL " << e.what() << "\n";
        return 1;
    }
}
