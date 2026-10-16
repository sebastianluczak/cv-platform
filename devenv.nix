{
  pkgs,
  lib,
  config,
  inputs,
  ...
}:

{
  packages = [ 
    pkgs.git
    pkgs.nodejs
    pkgs.antora
    pkgs.asciidoc
    pkgs.asciidoctor
    pkgs.go-task
    pkgs.gdb
    pkgs.pkg-config
    pkgs.cmake
    pkgs.meson
    pkgs.ninja
    pkgs.leptonica
    pkgs.tesseract4
    pkgs.vips
    pkgs.nats-server
    pkgs.grafana
  ];
  dotenv.enable = true;
  languages.go.enable = true;
  enterTest = ''
    echo "Starting devenv test..."
    sh test.sh
  '';
  services.rustfs = {
    enable = false;
  } // lib.optionalAttrs (!config.devenv.isTesting) {
    enable = true;
  };
  services.rabbitmq = {
    enable = false;
  } // lib.optionalAttrs (!config.devenv.isTesting) {
    enable = true;
    managementPlugin.enable = true;
  };
  services.prometheus = {
    enable = false;
  } // lib.optionalAttrs (!config.devenv.isTesting) {
    enable = true;
    scrapeConfigs = [
      {
        job_name = "aiweighter";
        static_configs = [
          {
            targets = [ "127.0.0.1:8051" ];
          }
        ];
      }
      {
        job_name = "datacleaner";
        static_configs = [
          {
            targets = [ "127.0.0.1:8052" ];
          }
        ];
      }
      {
        job_name = "extractor";
        static_configs = [
          {
            targets = [ "127.0.0.1:8053" ];
          }
        ];
      }
      {
        job_name = "offermatcher";
        static_configs = [
          {
            targets = [ "127.0.0.1:8054" ];
          }
        ];
      }
      {
        job_name = "piistripper";
        static_configs = [
          {
            targets = [ "127.0.0.1:8055" ];
          }
        ];
      }
      {
        job_name = "skillmatcher";
        static_configs = [
          {
            targets = [ "127.0.0.1:8056" ];
          }
        ];
      }
      {
        job_name = "roleinsights";
        static_configs = [
          {
            targets = [ "127.0.0.1:8058" ];
          }
        ];
      }
      {
        job_name = "uploader";
        static_configs = [
          {
            targets = [ "127.0.0.1:8057" ];
          }
        ];
      }
    ];
  };
  processes = { } // lib.optionalAttrs (!config.devenv.isTesting) {
    nats-jetstream = {
      exec = "nats-server --jetstream --store_dir .devenv/state/nats";
    };
    uploader = {
      exec = "sleep 3 && task services:uploader:start";
    };
    extractor = {
      exec = "sleep 3 && task services:extractor:start";
    };
    piistripper = {
      exec = "sleep 3 && task services:piistripper:start";
    };
    skillmatcher = {
      exec = "sleep 3 && task services:skillmatcher:start";
    };
    offermatcher = {
      exec = "sleep 3 && task services:offermatcher:start";
    };
    aiweighter = {
      exec = "sleep 3 && task services:aiweighter:start";
    };
    roleinsights = {
      exec = "sleep 3 && task services:roleinsights:start";
    };
    datacleaner = {
      exec = "sleep 8 && task services:datacleaner:start";
    };
    frontend = {
      exec = "task ui:install && task ui:start";
    };
    docs-builder = {
      exec = "task docs:build-docs";
    };
  };
}
