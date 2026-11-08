{
  description = "Ichor: Android build environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  # Swift 6.3.3 on nixos-unstable (since 2026-10-27) has swiftc marked broken on aarch64-linux;
  # the IchorCore Linux tests stay on the last revision with a working Swift (6.2.4) until it is
  # fixed. A pinned revision: `nix flake update` leaves it where it is.
  inputs.nixpkgs-swift.url = "github:NixOS/nixpkgs/494ce7fd23ff6a5dff39e1fb11e9b6f2ac74bf25";

  outputs = { nixpkgs, nixpkgs-swift, ... }:
    let
      nixpkgsConfig = {
        allowUnfree = true;
        android_sdk.accept_license = true;
      };

      buildToolsVersion = "37.0.0";
      ndkVersion = "29.0.14206865";

      # Android build-tools and the NDK only exist for x86_64 Linux. On aarch64 hosts they run
      # through binfmt emulation; everything else (Go, JDK, Gradle) stays native, because the
      # Go runtime is unreliable under qemu-user.
      sdkPkgs = import nixpkgs {
        system = "x86_64-linux";
        config = nixpkgsConfig;
        overlays = [ noI686SdkLibs ];
      };
      # androidenv's build-tools adds the i686 glibc, zlib and ncurses5 to autoPatchelf's inputs
      # on x86_64. Build-tools 37 has no 32-bit host binary (its only 32-bit ELF files are the
      # renderscript device libraries, which `autoPatchelf --no-recurse` never reads), and an
      # aarch64 host emulating x86_64 cannot build i686 when the binary cache lacks it (ncurses5
      # since 2026-10-27). Hand it the x86_64 libraries instead, on every host.
      noI686SdkLibs = final: prev: {
        pkgsi686Linux = prev.pkgsi686Linux // { inherit (final) glibc zlib ncurses5; };
      };
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
          packages = [ pkgs.jdk17 pkgs.gradle_9 pkgs.go_1_27 (pkgs.go-tools.override { buildGoModule = pkgs.buildGo127Module; }) ] # go-tools: staticcheck, built with the same Go
            # govulncheck scans with the `go` on PATH (go_1_27, whose standard library it checks).
            ++ [ pkgs.govulncheck ]
            # The on-device model engine (llama.cpp, built by build.sh with the NDK).
            ++ [ pkgs.cmake pkgs.ninja ]
            # Native cross-compiler for cgo on aarch64 hosts (matches NDK r29's clang 21).
            ++ pkgs.lib.optionals pkgs.stdenv.hostPlatform.isAarch64 [ pkgs.llvmPackages_21.clang-unwrapped pkgs.lld_21 ];

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
        let pkgs = import nixpkgs-swift { inherit system; };
        in {
          default = shellFor system;
          # Swift on Linux for the iOS core package tests (ios/IchorCore/test-linux.sh).
          swift = (pkgs.mkShell.override { stdenv = pkgs.swift.stdenv; }) {
            packages = [ pkgs.swift pkgs.swiftPackages.Foundation pkgs.swiftPackages.XCTest pkgs.swiftPackages.Dispatch ];
            LD_LIBRARY_PATH = "${pkgs.swiftPackages.Dispatch}/lib:${pkgs.swiftPackages.XCTest}/lib";
          };
        });
    };
}
