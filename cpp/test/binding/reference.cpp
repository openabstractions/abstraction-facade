// CONTRACT.md FAC-B4: a bound service carries the reference the runtime
// returned, read-only, so an application can name the provider that served it.
// bind_service supplies the reference and the transport explicitly, so this
// case needs no resolver and no provider endpoint.
#include <abstraction/facade/resolution.hpp>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

namespace f = abstraction::facade;
static void require(bool value, const char* what) { if (!value) throw std::runtime_error(what); }

// A transport the generated client is constructed with and never calls here.
struct SilentTransport {
    std::string exchange_frame(const std::string&) const {
        throw std::runtime_error("the reference is read without a call");
    }
    void write_frame(const std::string&) const {
        throw std::runtime_error("the reference is read without a call");
    }
};

int main() {
    try {
        f::ServiceReference served;
        served.provider = "openabstractions.user-runtime";
        served.capability = "abstraction.facade";
        served.contract = "abstraction.facade/registry@1";
        served.guarantees = {};
        served.scope = f::Scope::Local;
        served.transport = "oa-framed-local@1";
        served.endpoint = "registry-endpoint";

        auto bound = f::bind_service<f::RegistryService>(served, SilentTransport{}, {}, f::Scope::Local);
        const f::ServiceReference& carried = bound.reference();
        require(carried.provider == served.provider, "provider");
        require(carried.capability == served.capability, "capability");
        require(carried.contract == served.contract, "contract");
        require(carried.guarantees == served.guarantees, "guarantees");
        require(carried.scope == served.scope, "scope");
        require(carried.transport == served.transport, "transport");
        require(carried.endpoint == served.endpoint, "endpoint");

        // Read-only: the accessor hands back a const reference the caller
        // cannot assign through, and a moved binding keeps the same reference.
        static_assert(std::is_const_v<std::remove_reference_t<decltype(bound.reference())>>,
                      "reference() must be read-only");
        auto moved = std::move(bound);
        require(moved.reference().provider == served.provider, "a moved binding kept its reference");

        std::cout << "PASS a bound service names the provider that answered\n";
    } catch (const std::exception& e) {
        std::cerr << "FAIL " << e.what() << "\n";
        return 1;
    }
}
