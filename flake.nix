{
  description = "Talos Viewer: Android build environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { nixpkgs, ... }:
    let
      nixpkgsConfig = {
        allowUnfree = true;
        android_sdk.accept_license = true;
      };

      buildToolsVersion = "37.0.0";
      ndkVersion = "27.2.12479018";

      # Android build-tools and the NDK only exist for x86_64 Linux. On aarch64 hosts they run
      # through binfmt emulation; everything else (Go, JDK, Gradle) stays native, because the
      # Go runtime is unreliable under qemu-user.
      sdkPkgs = import nixpkgs { system = "x86_64-linux"; config = nixpkgsConfig; };
      android = sdkPkgs.androidenv.composeAndroidPackages {
        platformVersions = [ "37" ];
        buildToolsVersions = [ buildToolsVersion ];
        includeNDK = true;
        ndkVersions = [ ndkVersion ];
        includeEmulator = false;
        includeSystemImages = false;
      };
      sdkRoot = "${android.androidsdk}/libexec/android-sdk";

      shellFor = system:
        let pkgs = import nixpkgs { inherit system; config = nixpkgsConfig; };
        in pkgs.mkShell {
          packages = [ pkgs.jdk17 pkgs.gradle_9 pkgs.go ]
            # Native cross-compiler for cgo on aarch64 hosts (matches NDK r27's clang 18).
            ++ pkgs.lib.optionals pkgs.stdenv.hostPlatform.isAarch64 [ pkgs.llvmPackages_18.clang-unwrapped pkgs.lld_18 ];

          ANDROID_HOME = sdkRoot;
          ANDROID_SDK_ROOT = sdkRoot;
          ANDROID_NDK_HOME = "${sdkRoot}/ndk/${ndkVersion}";
          JAVA_HOME = pkgs.jdk17.home;
          # The aapt2 Gradle downloads from Maven is a generic glibc binary that cannot run on NixOS.
          GRADLE_OPTS = "-Dorg.gradle.project.android.aapt2FromMavenOverride=${sdkRoot}/build-tools/${buildToolsVersion}/aapt2";
        };
    in
    {
      devShells = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" ] (system:
        let pkgs = import nixpkgs { inherit system; };
        in {
          default = shellFor system;
          # Swift on Linux for the iOS core package tests (ios/TalosViewerCore/test-linux.sh).
          swift = (pkgs.mkShell.override { stdenv = pkgs.swift.stdenv; }) {
            packages = [ pkgs.swift pkgs.swiftPackages.Foundation pkgs.swiftPackages.XCTest pkgs.swiftPackages.Dispatch ];
            LD_LIBRARY_PATH = "${pkgs.swiftPackages.Dispatch}/lib:${pkgs.swiftPackages.XCTest}/lib";
          };
        });
    };
}
