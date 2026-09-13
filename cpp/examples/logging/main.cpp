#include <abstraction/facade/client.hpp>
#include <iostream>

int main() {
    try {
        auto events = abstraction::facade::Discover().ResolveLog();
        events.Log(0, "worker started", {{"component", "worker"}});
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
