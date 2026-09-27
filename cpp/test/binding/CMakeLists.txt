cmake_minimum_required(VERSION 3.16)
project(facade_binding_consumer LANGUAGES CXX)
find_package(abstraction_facade CONFIG REQUIRED)
add_executable(facade_binding_consumer main.cpp)
target_link_libraries(facade_binding_consumer PRIVATE abstraction::facade_client)
enable_testing()
add_test(NAME facade_binding_consumer COMMAND facade_binding_consumer)
set_tests_properties(facade_binding_consumer PROPERTIES TIMEOUT 30)

if(WIN32)
    add_executable(facade_bootstrap_failure bootstrap_failure.cpp)
    target_link_libraries(facade_bootstrap_failure PRIVATE abstraction::facade_jobs)
    add_test(NAME facade_bootstrap_failure COMMAND facade_bootstrap_failure)
    add_test(NAME facade_bootstrap_native COMMAND ${CMAKE_COMMAND} -E env
        "ABSTRACTION_RUNTIME_ENDPOINT=" $<TARGET_FILE:facade_binding_consumer> --default-runtime-endpoint)
    set_tests_properties(facade_bootstrap_native PROPERTIES PASS_REGULAR_EXPRESSION "openabstractions-user-S-1-[0-9-]+-runtime-v1")
    add_executable(facade_resolution_error resolution_error.cpp)
    target_link_libraries(facade_resolution_error PRIVATE abstraction::facade_client)
    add_test(NAME facade_resolution_error COMMAND facade_resolution_error)
    set_tests_properties(facade_resolution_error PROPERTIES TIMEOUT 30)
endif()
add_test(NAME facade_bootstrap_override COMMAND ${CMAKE_COMMAND} -E env
    "ABSTRACTION_RUNTIME_ENDPOINT=explicit-bootstrap" $<TARGET_FILE:facade_binding_consumer> --default-runtime-endpoint)
if(APPLE)
    # Installed macOS selection uses the fixed LaunchAgent Mach service.
    set_tests_properties(facade_bootstrap_override PROPERTIES PASS_REGULAR_EXPRESSION "xpc:com.openabstractions.runtime-v1")
else()
    set_tests_properties(facade_bootstrap_override PROPERTIES PASS_REGULAR_EXPRESSION "explicit-bootstrap")
endif()

add_executable(facade_platform_refusal platform_refusal.cpp)
target_link_libraries(facade_platform_refusal PRIVATE abstraction::facade_client)
add_test(NAME facade_platform_refusal COMMAND facade_platform_refusal)

add_executable(facade_platform_macos platform_macos.cpp)
target_link_libraries(facade_platform_macos PRIVATE abstraction::facade_client)
add_test(NAME facade_platform_macos COMMAND facade_platform_macos)

add_executable(facade_deadline_no_io deadline_no_io.cpp)
target_link_libraries(facade_deadline_no_io PRIVATE abstraction::facade_client)
add_test(NAME facade_deadline_no_io COMMAND facade_deadline_no_io)

# Windows runs a copy of this executable as a stand-in openabstractions.exe
# under the test's working directory; other platforms check the activation
# steps and that the SDK does not activate.
add_executable(facade_activation activation.cpp)
target_link_libraries(facade_activation PRIVATE abstraction::facade_client)
add_test(NAME facade_activation COMMAND facade_activation WORKING_DIRECTORY ${CMAKE_CURRENT_BINARY_DIR})
set_tests_properties(facade_activation PROPERTIES TIMEOUT 90)

# The resolved reference a bound service carries (CONTRACT.md FAC-B4). It uses
# an explicit reference and transport, so it needs no resolver.
add_executable(facade_reference reference.cpp)
target_link_libraries(facade_reference PRIVATE abstraction::facade_resolution)
add_test(NAME facade_reference COMMAND facade_reference)
set_tests_properties(facade_reference PROPERTIES TIMEOUT 30)

add_executable(facade_server_trust server_trust.cpp)
target_link_libraries(facade_server_trust PRIVATE abstraction::facade_client)
add_test(NAME facade_server_trust COMMAND facade_server_trust)
