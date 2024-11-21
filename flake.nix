{
  description = "Android Development";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };
  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config = {
            android_sdk.accept_license = true;
            allowUnfree = true;
          };
        };
        
        buildToolsVersion = "33.0.2";
        
        # Compose Android SDK and NDK
        androidComposition = pkgs.androidenv.composeAndroidPackages {
          cmdLineToolsVersion = "8.0";
          toolsVersion = "26.1.1";
          platformToolsVersion = "33.0.3";
          buildToolsVersions = [ "33.0.2" ];
          includeEmulator = false;
          platformVersions = [ "28" "29" "30" ];
          includeSources = false;
          includeSystemImages = false;
          systemImageTypes = [ "google_apis_playstore" ];
          abiVersions = [ "armeabi-v7a" "arm64-v8a" ];
          cmakeVersions = [ "3.10.2" ];
          includeNDK = true;
          ndkVersions = ["22.0.7026061"];
          useGoogleAPIs = false;
          useGoogleTVAddOns = false;
          includeExtras = [
            "extras;google;gcm"
          ];
        };
        
        androidSdk = androidComposition.androidsdk;
      in
      {
        # Define a devShell for ARMv7 cross-compilation
        devShell = with pkgs; mkShell {
          ANDROID_SDK_ROOT = "${androidComposition.androidsdk}/libexec/android-sdk";
          ANDROID_NDK_ROOT = "${androidComposition.androidsdk}/libexec/android-sdk/ndk-bundle";
          ANDROID_NDK_HOME = "${androidComposition.androidsdk}/libexec/android-sdk/ndk-bundle";
          LD_LIBRARY_PATH = "${pkgs.libglvnd}/lib";
          buildInputs = [
            androidSdk
            jdk11
						go
						pkg-config
						# zlib
						gnumake

						# protobuf
						protoc-gen-go
						protobuf

						# perf
						graphviz

						# GUI dependencies
						fyne
						libGL 
						pkg-config 
						xorg.libX11.dev 
						xorg.libXcursor 
						xorg.libXi 
						xorg.libXinerama 
						xorg.libXrandr 
						xorg.libXxf86vm
          ];
        };
      });
}

