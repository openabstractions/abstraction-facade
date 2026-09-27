#define ABSTRACTION_FACADE_TARGET_PLATFORM "macos"
#include <abstraction/facade/resolution.hpp>
#include <iostream>

int main() {
    if (!abstraction::facade::unsupported_platform().empty()) return 1;
    std::cout << "PASS macOS proceeds to installed runtime selection\n";
    return 0;
}
