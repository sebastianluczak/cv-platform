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
  ];
  languages.go.enable = true;
  services ={
    rustfs = {
      enable = true;
    };
  };
  scripts = {
    build-docs = {
      exec = "cd docs && antora antora-playbook.yml";
    };
  };
  processes = {
    backend = {
      exec = "go -C services run .";
    };
    frontend = {
      exec = "cd ui && npm ci && npm run dev";
    };
    docs-builder = {
      exec = "build-docs";
    };
  };
}
