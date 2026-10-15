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
    frontend = {
      exec = "task ui:install && task ui:start";
    };
    docs-builder = {
      exec = "task docs:build-docs";
    };
  };
}
