#include <abstraction/facade/client.hpp>
#include <iostream>

int main() {
    try {
        auto events = abstraction::facade::discover().resolve_log();
        events.log(0, "worker started", {{"component", "worker"}});
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
