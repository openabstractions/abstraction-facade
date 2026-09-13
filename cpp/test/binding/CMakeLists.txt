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
endif()
add_test(NAME facade_bootstrap_override COMMAND ${CMAKE_COMMAND} -E env
    "ABSTRACTION_RUNTIME_ENDPOINT=explicit-bootstrap" $<TARGET_FILE:facade_binding_consumer> --default-runtime-endpoint)
set_tests_properties(facade_bootstrap_override PROPERTIES PASS_REGULAR_EXPRESSION "explicit-bootstrap")

add_executable(facade_deadline_no_io deadline_no_io.cpp)
target_link_libraries(facade_deadline_no_io PRIVATE abstraction::facade_client)
add_test(NAME facade_deadline_no_io COMMAND facade_deadline_no_io)

add_executable(facade_server_trust server_trust.cpp)
target_link_libraries(facade_server_trust PRIVATE abstraction::facade_client)
add_test(NAME facade_server_trust COMMAND facade_server_trust)
