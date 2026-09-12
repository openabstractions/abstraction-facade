#include <abstraction/facade/rec.h>

namespace wire = abstraction::facade;

// This is a dispatcher fixture, not a production C++ server.
struct Provider : wire::Resolver {
    wire::ResolveResult Resolve(const wire::ResolveRequest& request) override {
        if (request.capability != "example.work" || request.contracts.size() != 1)
            throw std::runtime_error("request lost meaning");
        wire::ResolveResult result;
        result.status = "unavailable";
        return result;
    }
};

int main() {
    Provider provider;
    wire::ResolverDispatcher transport(provider);
    wire::ResolverClient<wire::ResolverDispatcher> client(transport);
    wire::ResolveRequest request;
    request.capability = "example.work";
    request.contracts = {"example.work/runner@1"};
    request.scope = "any";
    auto result = client.Resolve(request);
    if (result.status != "unavailable" || result.reference) return 1;
    request.scope = "invented";
    try { client.Resolve(request); return 2; }
    catch (const wire::Refusal& refusal) {
        if (std::string_view(refusal.word) != "bad_enum") return 3;
    }
    return 0;
}
