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
  languages.go.enable = true;
  enterShell = ''
    export $(cat .env | xargs)
  '';
  services.rustfs.enable = true;
  processes = {
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
