cmake_minimum_required(VERSION 3.16)
project(OneLLMRouterDesktop VERSION 1.4.1 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)
set(ONELLM_VERSION "1.4.1" CACHE STRING "OneLLMRouter release version")

include(CTest)
find_package(Qt6 REQUIRED COMPONENTS Widgets Network)
if(BUILD_TESTING)
    find_package(Qt6 REQUIRED COMPONENTS Test)
endif()

add_executable(onellm-router-tray WIN32
    src/main.cpp
    src/proxy_probe.cpp
    src/proxy_probe.h
    src/router_discovery.cpp
    src/router_discovery.h
    src/router_process.cpp
    src/router_process.h
    src/router_types.h
    src/smoke_mode.cpp
    src/smoke_mode.h
    src/tray_application.cpp
    src/tray_application.h
    src/i18n.h
    assets/icons.qrc
)
target_link_libraries(onellm-router-tray PRIVATE Qt6::Widgets Qt6::Network)
target_compile_definitions(onellm-router-tray PRIVATE ONELLM_VERSION="${ONELLM_VERSION}")

if(BUILD_TESTING)
    add_subdirectory(tests)
endif()
