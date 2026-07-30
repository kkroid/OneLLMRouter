add_executable(router_types_test router_types_test.cpp)
target_include_directories(router_types_test PRIVATE ../src)
target_link_libraries(router_types_test PRIVATE Qt6::Test)
add_test(NAME router_types_test COMMAND router_types_test)
set_tests_properties(router_types_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(router_discovery_test
    router_discovery_test.cpp
    ../src/router_discovery.cpp
    ../src/router_discovery.h
)
target_include_directories(router_discovery_test PRIVATE ../src)
target_link_libraries(router_discovery_test PRIVATE Qt6::Test Qt6::Network)
add_test(NAME router_discovery_test COMMAND router_discovery_test)
set_tests_properties(router_discovery_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(router_process_test
    router_process_test.cpp
    ../src/router_process.cpp
    ../src/router_process.h
)
target_include_directories(router_process_test PRIVATE ../src)
target_link_libraries(router_process_test PRIVATE Qt6::Test Qt6::Network)
add_test(NAME router_process_test COMMAND router_process_test)
set_tests_properties(router_process_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(version_contract_test version_contract_test.cpp)
target_link_libraries(version_contract_test PRIVATE Qt6::Test)
target_compile_definitions(version_contract_test PRIVATE
    ONELLM_VERSION="${ONELLM_VERSION}"
)
add_test(NAME version_contract_test COMMAND version_contract_test)
set_tests_properties(version_contract_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(proxy_probe_test
    proxy_probe_test.cpp
    ../src/proxy_probe.cpp
    ../src/proxy_probe.h
)
target_include_directories(proxy_probe_test PRIVATE ../src)
target_link_libraries(proxy_probe_test PRIVATE Qt6::Test Qt6::Network)
add_test(NAME proxy_probe_test COMMAND proxy_probe_test)
set_tests_properties(proxy_probe_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)
