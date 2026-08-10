add_executable(test_core_fixture test_core_fixture.cpp)
target_link_libraries(test_core_fixture PRIVATE Qt6::Core)

find_program(ONELLM_GO_EXECUTABLE go)
if(NOT ONELLM_GO_EXECUTABLE)
    message(FATAL_ERROR "Go is required to build the test Core")
endif()
set(ONELLM_TEST_CORE
    "${CMAKE_CURRENT_BINARY_DIR}/onellm-router-test-core${CMAKE_EXECUTABLE_SUFFIX}")
add_custom_target(onellm_test_core ALL
    COMMAND "${ONELLM_GO_EXECUTABLE}" build -o "${ONELLM_TEST_CORE}"
            ./cmd/onellm-router
    WORKING_DIRECTORY "${CMAKE_SOURCE_DIR}/.."
    BYPRODUCTS "${ONELLM_TEST_CORE}"
    COMMENT "Building test Core"
)

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
    ../src/platform/platform.h
    ../${ONELLM_PLATFORM_SOURCE}
)
target_include_directories(router_discovery_test PRIVATE ../src)
target_link_libraries(router_discovery_test PRIVATE Qt6::Test Qt6::Network)
add_dependencies(router_discovery_test test_core_fixture)
add_test(NAME router_discovery_test COMMAND router_discovery_test)
set_tests_properties(router_discovery_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(router_process_test
    router_process_test.cpp
    ../src/router_process.cpp
    ../src/router_process.h
    ../src/platform/platform.h
    ../${ONELLM_PLATFORM_SOURCE}
)
target_include_directories(router_process_test PRIVATE ../src)
target_link_libraries(router_process_test PRIVATE Qt6::Test Qt6::Network)
add_dependencies(router_process_test test_core_fixture)
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

add_executable(tray_application_test
    tray_application_test.cpp
    ../src/main_window.cpp
    ../src/main_window.h
    ../src/config/config_client.cpp
    ../src/config/config_client.h
    ../src/clients/clients_model.cpp
    ../src/clients/clients_page.cpp
    ../src/usage/usage_model.cpp
    ../src/usage/usage_model.h
    ../src/usage/usage_page.cpp
    ../src/usage/usage_page.h
    ../src/proxy_probe.cpp
    ../src/router_discovery.cpp
    ../src/router_process.cpp
    ../src/tray_application.cpp
    ../${ONELLM_PLATFORM_SOURCE}
    ../assets/icons.qrc
)
target_include_directories(tray_application_test PRIVATE ../src)
target_link_libraries(tray_application_test PRIVATE Qt6::Test Qt6::Widgets Qt6::Network)
target_compile_definitions(tray_application_test PRIVATE ONELLM_VERSION="${ONELLM_VERSION}")
add_test(NAME tray_application_test COMMAND tray_application_test)
set_tests_properties(tray_application_test PROPERTIES
    ENVIRONMENT "QT_QPA_PLATFORM=offscreen"
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(smoke_mode_test
    smoke_mode_test.cpp
    ../src/smoke_mode.cpp
    ../src/router_discovery.cpp
    ../src/router_process.cpp
    ../${ONELLM_PLATFORM_SOURCE}
)
target_include_directories(smoke_mode_test PRIVATE ../src)
target_link_libraries(smoke_mode_test PRIVATE Qt6::Test Qt6::Network)
add_test(NAME smoke_mode_test COMMAND smoke_mode_test)
set_tests_properties(smoke_mode_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(platform_test
    platform_test.cpp
    ../src/platform/platform.h
    ../${ONELLM_PLATFORM_SOURCE}
)

add_executable(config_client_test
    config_client_test.cpp
    ../src/config/config_client.cpp
    ../src/config/config_client.h
    ../src/clients/clients_model.cpp
)
target_include_directories(config_client_test PRIVATE ../src)
target_link_libraries(config_client_test PRIVATE Qt6::Test Qt6::Network)
add_dependencies(config_client_test onellm_test_core)
add_test(NAME config_client_test COMMAND config_client_test)
set_tests_properties(config_client_test PROPERTIES
    ENVIRONMENT "ONELLM_TEST_CORE=${ONELLM_TEST_CORE}"
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(clients_model_test
    clients_model_test.cpp
    ../src/clients/clients_model.cpp
)
target_include_directories(clients_model_test PRIVATE ../src)
target_link_libraries(clients_model_test PRIVATE Qt6::Test)
add_test(NAME clients_model_test COMMAND clients_model_test)
set_tests_properties(clients_model_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(clients_page_test
    clients_page_test.cpp
    ../src/clients/clients_model.cpp
    ../src/clients/clients_page.cpp
    ../src/config/config_client.cpp
)
target_include_directories(clients_page_test PRIVATE ../src)
target_link_libraries(clients_page_test PRIVATE Qt6::Test Qt6::Widgets Qt6::Network)
add_dependencies(clients_page_test onellm_test_core)
add_test(NAME clients_page_test COMMAND clients_page_test)
set_tests_properties(clients_page_test PROPERTIES
    ENVIRONMENT "QT_QPA_PLATFORM=offscreen;ONELLM_TEST_CORE=${ONELLM_TEST_CORE}"
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(main_window_test
    main_window_test.cpp
    ../src/main_window.cpp
    ../src/main_window.h
    ../src/config/config_client.cpp
    ../src/config/config_client.h
    ../src/clients/clients_model.cpp
    ../src/clients/clients_page.cpp
    ../src/clients/clients_page.h
    ../src/usage/usage_model.cpp
    ../src/usage/usage_model.h
    ../src/usage/usage_page.cpp
    ../src/usage/usage_page.h
    ../src/platform/platform.h
    ../${ONELLM_PLATFORM_SOURCE}
)
target_include_directories(main_window_test PRIVATE ../src)
target_link_libraries(main_window_test PRIVATE Qt6::Test Qt6::Widgets Qt6::Network)
add_dependencies(main_window_test onellm_test_core)
add_test(NAME main_window_test COMMAND main_window_test)
set_tests_properties(main_window_test PROPERTIES
    ENVIRONMENT "QT_QPA_PLATFORM=offscreen;ONELLM_TEST_CORE=${ONELLM_TEST_CORE}"
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(usage_model_test
    usage_model_test.cpp
    ../src/usage/usage_model.cpp
    ../src/usage/usage_model.h
)
target_include_directories(usage_model_test PRIVATE ../src)
target_link_libraries(usage_model_test PRIVATE Qt6::Test Qt6::Gui)
add_dependencies(usage_model_test onellm_test_core)
add_test(NAME usage_model_test COMMAND usage_model_test)
set_tests_properties(usage_model_test PROPERTIES
    ENVIRONMENT "ONELLM_TEST_CORE=${ONELLM_TEST_CORE}"
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)

add_executable(usage_page_test
    usage_page_test.cpp
    ../src/usage/usage_model.cpp
    ../src/usage/usage_model.h
    ../src/usage/usage_page.cpp
    ../src/usage/usage_page.h
)
target_include_directories(usage_page_test PRIVATE ../src)
target_link_libraries(usage_page_test PRIVATE Qt6::Test Qt6::Widgets)
add_test(NAME usage_page_test COMMAND usage_page_test)
set_tests_properties(usage_page_test PROPERTIES
    ENVIRONMENT "QT_QPA_PLATFORM=offscreen"
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)
target_include_directories(platform_test PRIVATE ../src)
target_link_libraries(platform_test PRIVATE Qt6::Test)
add_test(NAME platform_test COMMAND platform_test)
set_tests_properties(platform_test PROPERTIES
    ENVIRONMENT_MODIFICATION "PATH=path_list_prepend:$<TARGET_FILE_DIR:Qt6::Core>"
)
