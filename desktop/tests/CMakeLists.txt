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
