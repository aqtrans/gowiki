{ config, lib, pkgs, ... }:

with lib;

let 
  cfg = config.services.gowiki;
  pkgDesc = "Simple wiki written in Go, using Git";

  confFile = pkgs.writeText "gowiki.toml" ''
    GitPath = ""
    GitCommitEmail = "${cfg.gitEmail}"
    GitCommitName = "${cfg.gitName}" 
    DataDir = "${cfg.stateDir}"
    WikiDir = "${cfg.wikiDir}" 
    Port = "${cfg.listenPort}"    
    Domain = "${cfg.domainName}"
    RemoteGitRepo = "${cfg.remoteGitRepo}"
    PushOnSave = ${boolToString cfg.pushOnSave}
    InitWikiRepo = ${boolToString cfg.initWikiRepo}
    CacheEnabled = ${boolToString cfg.cacheEnabled}
    CSRF = ${boolToString cfg.csrfEnabled}
    DebugMode = ${boolToString cfg.debugMode}
  '';

in {

  options = {

    services.gowiki = {

      enable = mkEnableOption "${pkgName}";

      user = mkOption {
        type = types.str;
        default = "gowiki";
        description = "gowiki user";
      };

      group = mkOption {
        type = types.str;
        default = "gowiki";
        description = "gowiki group";
      };

      stateDir = mkOption {
        type = types.path;
        default = "/var/lib/gowiki/";
        description = "state directory for gowiki";
        example = "/home/user/.gowiki/";
      };

      wikiDir = mkOption {
        type = types.str;
        default = "";
        description = "where to store wiki git repo";
        example = "/home/user/.gowiki/wikidata";
      };    

      listenPort = mkOption {
        type = types.str;
        default = "8001";
        description = "TCP port to listen on";
      };         

      gitEmail = mkOption {
        type = types.str;
        default = "gowiki@nixos";
        description = "Email used for Git";
      };

      gitName = mkOption {
        type = types.str;
        default = "Gowiki";
        description = "Name used for Git";
      };      

      domainName = mkOption {
        type = types.str;
        default = "wiki.example.com";
        description = "Domain name of gowiki";
      };  

      remoteGitRepo = mkOption {
        type = types.str;
        default = "";
        description = "Remote Git repository to sync with";
      }; 

      pushOnSave = mkOption {
        type = types.bool;
        default = true;
        description = "Push to remote git repo after saving pages";
      }; 

      initWikiRepo = mkOption {
        type = types.bool;
        default = true;
        description = "Create new wiki git repo if one does not already exist";
      }; 

      cacheEnabled = mkOption {
        type = types.bool;
        default = true;
        description = "Enable a cache of pages to speed up page loads";
      }; 

      csrfEnabled = mkOption {
        type = types.bool;
        default = true;
        description = "Enable CSRF protection. Can be disabled for debugging/development";
      };

      debugMode = mkOption {
        type = types.bool;
        default = false;
        description = "Enable debug logging and disable certain security protections";
      };

    };

  };

  config = mkIf cfg.enable {

    users.users.${cfg.user} = {
      name = cfg.user;
      group = cfg.group;
      home = cfg.stateDir;
      isSystemUser = true;
      createHome = true;
      description = pkgDesc;
    };

    users.groups.${cfg.user} = {
      name = cfg.group;
    };

    systemd.services.gowiki = {
      description = pkgDesc;
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        Restart = "always";
        ProtectSystem = "strict";
        ReadWritePaths = ''${confFile} ${cfg.stateDir}'';
        WorkingDirectory = cfg.stateDir;
        ExecStart = ''
          ${pkgs.gowiki}/bin/wiki -conf ${confFile}
        '';
      };
    };

  };

}
