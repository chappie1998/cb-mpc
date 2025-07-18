macro(link_openssl TARGET_NAME)
  if(IS_LINUX)
    set(OPENSSL_PATH "/usr/local/opt/openssl@3.2.0")
    include_directories(${OPENSSL_PATH}/include)
    target_link_libraries(${TARGET_NAME} PUBLIC ${OPENSSL_PATH}/lib64/libcrypto.a)
  endif()

  if(IS_MACOS)
    # Use Homebrew OpenSSL on macOS
    set(OPENSSL_PATH "/opt/homebrew/opt/openssl@3")
    include_directories(${OPENSSL_PATH}/include)
    target_link_libraries(${TARGET_NAME} PUBLIC 
                          ${OPENSSL_PATH}/lib/libcrypto.a
                          ${OPENSSL_PATH}/lib/libssl.a)
  endif()
endmacro(link_openssl)
