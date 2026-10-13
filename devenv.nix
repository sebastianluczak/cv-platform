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
    pkgs.go-task
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
  processes = { } // lib.optionalAttrs (!config.devenv.isTesting) {
    backend = {
      exec = "task services:start";
    };
    frontend = {
      exec = "task ui:install && task ui:start";
    };
    docs-builder = {
      exec = "task docs:build-docs";
    };
  };
}
