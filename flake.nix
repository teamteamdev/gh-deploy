{
  description = "GitHub webhook-based deployment system";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = import nixpkgs {inherit system;};
      in {
        packages = rec {
          gh-deploy = pkgs.buildGoModule {
            pname = "gh-deploy";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-C6v/2VamvK2C/YPqtNLq91v0hkCge8akOaKp/Yy2TpM=";
          };
          default = gh-deploy;
        };

        apps.default = flake-utils.lib.mkApp {
          drv = self.packages.${system}.default;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            go-tools
          ];
        };
      }
    )
    // {
      nixosModules.default = {
        config,
        lib,
        pkgs,
        ...
      }: let
        cfg = config.services.gh-deploy;

        configFile = {
          bind = "unix:/run/gh-deploy/http.sock";
          git_lfs = cfg.gitLfs;
          github_app = {
            client_id = cfg.githubApp.clientId;
            private_key_file = "$CREDENTIALS_DIRECTORY/private_key";
            webhook_secret_file = "$CREDENTIALS_DIRECTORY/webhook_secret";
          };
          projects = lib.mapAttrsToList (path: project: {
            inherit path;
            inherit (project) repository branch timeout command;
          }) cfg.projects;
        };

        tomlFormatter = pkgs.formats.toml {};
        configFileFormatted = tomlFormatter.generate "config.toml" configFile;

      in {
        options.services.gh-deploy = {
          enable = lib.mkEnableOption "gh-deploy";

          domain = lib.mkOption {
            type = lib.types.str;
            description = "Domain for gh-deploy.";
          };

          gitLfs = lib.mkOption {
            type = lib.types.bool;
            default = false;
            description = "Enable Git LFS support.";
          };

          githubApp = lib.mkOption {
            type = lib.types.submodule {
              options = {
                clientId = lib.mkOption {
                  type = lib.types.str;
                  description = "GitHub App Client ID.";
                };
                privateKeyFile = lib.mkOption {
                  type = lib.types.path;
                  description = "GitHub App private key file.";
                };
                webhookSecretFile = lib.mkOption {
                  type = lib.types.path;
                  description = "File with webhook secret.";
                };
              };
            };
            description = "GitHub App configuration.";
          };

          projects = lib.mkOption {
            type = lib.types.attrsOf (lib.types.submodule {
              options = {
                repository = lib.mkOption {
                  type = lib.types.str;
                  description = "GitHub repository to deploy.";
                };
                branch = lib.mkOption {
                  type = lib.types.str;
                  description = "Branch to deploy.";
                };
                timeout = lib.mkOption {
                  type = lib.types.int;
                  default = 120;
                  description = "Timeout for deployment in seconds.";
                };
                command = lib.mkOption {
                  type = lib.types.str;
                  description = "Script to run after pulling updates.";
                };
              };
            });
            description = "Repositories and branches to deploy.";
          };
        };

        config = lib.mkIf cfg.enable {
          environment.etc."gh-deploy/config.toml".source = configFileFormatted;

          users.extraUsers.gh-deploy = {
            isSystemUser = true;
            group = "gh-deploy";
            home = "/var/lib/gh-deploy";
            createHome = true;
            useDefaultShell = true;
          };
          users.groups.gh-deploy = {};

          systemd.services.gh-deploy = {
            description = "GitHub webhook-based deployment system";
            wantedBy = ["multi-user.target"];
            wants = ["network-online.target"];
            after = ["network-online.target"];

            reloadTriggers =  [ config.environment.etc."gh-deploy/config.toml".source ];

            serviceConfig = {
              User = "gh-deploy";
              Group = "gh-deploy";
              WorkingDirectory = "/var/lib/gh-deploy";
              RuntimeDirectory = "gh-deploy";
              AmbientCapabilities = [ "CAP_NET_BIND_SERVICE" ];
              LoadCredential = [
                "webhook_secret:${cfg.githubApp.webhookSecretFile}"
                "private_key:${cfg.githubApp.privateKeyFile}"
              ];
              ExecReload = "${lib.getBin pkgs.coreutils}/bin/kill -HUP $MAINPID";
            };

            path =
              [self.packages.${pkgs.system}.gh-deploy pkgs.git]
              ++ lib.optional cfg.gitLfs pkgs.git-lfs;
            script = ''
              exec ${self.packages.${pkgs.system}.gh-deploy}/bin/gh-deploy /etc/gh-deploy/config.toml
            '';
          };

          services.nginx = {
            enable = true;

            virtualHosts."${cfg.domain}" = {
              forceSSL = true;
              enableACME = true;
              locations."/".proxyPass = "http://unix:/run/gh-deploy/http.sock";
            };
          };
        };
      };
    };
}
