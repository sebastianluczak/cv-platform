{
  pkgs,
  lib,
  config,
  inputs,
  ...
}:

{
  packages = [ pkgs.git pkgs.nodejs ];
  languages.go.enable = true;
  services ={
    rustfs = {
      enable = true;
    };
  };
  processes = {
    backend = {
      exec = "go -C services run .";
    };
    frontend = {
      exec = "cd ui && npm ci && npm run dev";
    };
  };
}
