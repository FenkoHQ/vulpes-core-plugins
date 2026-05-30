{
  description = "Vulpes Core plugin bundle";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
  let
    systems = [ "x86_64-linux" "aarch64-linux" ];
    forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

    # vendorHash per plugin. Every plugin requires the in-repo sdk (a local
    # replace), so each needs a vendor dir — null won't do. Plugins with no
    # external deps share one hash (they vendor only the sdk). Regenerate after
    # a dependency change with:
    #   nix build .#<plugin> 2>&1 | grep got:
    sdkOnly = "sha256-qPFBG7QPYh9Mtx2aMiLNcC1UtaD6fBVivaL96p503v0=";
    vendorHashes = {
      authn-static-api-key = sdkOnly;
      authn-postgres-api-key = "sha256-D8SieJ02fhtKrbHDVOb+APYYMV+IKxFmnsznKf3TXeE=";
      cache-memory = sdkOnly;
      ratelimit-memory = sdkOnly;
      router-weighted = sdkOnly;
      router-litellm = sdkOnly;
      router-consul = sdkOnly;
      prompt-context-injector = sdkOnly;
      prompt-template-registry = sdkOnly;
      upstream-openai = sdkOnly;
      upstream-codex = sdkOnly;
      observer-stdout = sdkOnly;
      observer-prometheus = sdkOnly;
      observer-otel = "sha256-z/DDxOJZlDSzek1IjhM80j8GiT2hfdU59NggU19eEoo=";
      observer-s3-transcripts = "sha256-BtPGLTPoPuW/HIbJQMw6C9S9Pau28FO/qCNc6nbcyqA=";
    };
  in {
    packages = forAllSystems (pkgs:
      let
        buildPlugin = name: pkgs.buildGoModule {
          pname = name;
          version = "0.1.0";
          src = self;
          modRoot = "plugins/${name}";
          vendorHash = vendorHashes.${name};
          subPackages = [ "." ];
          # Static, stripped binaries suitable for deployment.
          env = {
            # Repo root carries a go.work; vendoring must ignore workspace mode.
            GOWORK = "off";
            CGO_ENABLED = "0";
          };
          ldflags = [ "-s" "-w" ];
          doCheck = false;
        };
        plugins = nixpkgs.lib.mapAttrs (name: _: buildPlugin name) vendorHashes;
      in plugins // {
        plugin-bundle = pkgs.symlinkJoin {
          name = "vulpes-core-plugin-bundle";
          paths = builtins.attrValues plugins;
        };
        default = self.packages.${pkgs.system}.plugin-bundle;
      });
  };
}
