{
  description = "A basic flake with a shell";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  inputs.flake-utils.url = "github:numtide/flake-utils";

  outputs = {
    nixpkgs,
    flake-utils,
    ...
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        devShells.default = pkgs.mkShell {
					packages = with pkgs; [
						go
						templ
						pkg-config
						zlib
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
      }
    );
}
