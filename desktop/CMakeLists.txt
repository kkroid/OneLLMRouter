cmake_minimum_required(VERSION 3.16)
project(OneLLMRouterDesktop VERSION 1.4.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)
set(ONELLM_VERSION "1.4.0" CACHE STRING "OneLLMRouter release version")

include(CTest)
find_package(Qt6 REQUIRED COMPONENTS Widgets Network Test)

add_executable(onellm-router-tray WIN32
    src/main.cpp
    src/proxy_probe.cpp
    src/proxy_probe.h
    src/router_discovery.cpp
    src/router_discovery.h
    src/router_process.cpp
    src/router_process.h
    src/router_types.h
)
target_link_libraries(onellm-router-tray PRIVATE Qt6::Widgets Qt6::Network)
target_compile_definitions(onellm-router-tray PRIVATE ONELLM_VERSION="${ONELLM_VERSION}")

if(BUILD_TESTING)
    add_subdirectory(tests)
endif()
