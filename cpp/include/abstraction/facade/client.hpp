#pragma once
#include <abstraction/logging/client.hpp>
#include <abstraction/config/client.hpp>
#include <abstraction/router/client.hpp>

namespace abstraction::facade {
// Resolves service clients lazily. The requested operation reports absence;
// discovery never creates a store or substitutes an embedded provider.
class Machine {
public:
    logging::Logger Log() const { return logging::Logger{}; }
    config::Client Config() const { return config::Client{}; }
    router::Client Router() const { return router::Client{}; }
};
inline Machine Discover() { return {}; }
}
