{
  config,
  lib,
  pkgs,
  ...
}:

let
  # Adapted from `nixpkgs/nixos/modules/config/locale.nix`
  nospace = str: lib.filter (c: c == " ") (lib.stringToCharacters str) == [ ];
  tzOverrideType = lib.types.nullOr (lib.types.addCheck lib.types.str nospace) // {
    description = "null or string without spaces";
  };
in

with lib;

{
  options.etc = {
    timezoneOverride = mkOption {
      default = "UTC";
      type = tzOverrideType;
      example = "America/New_York";
      description = ''
        The timezone in the sandbox.  If null, the system timezone will be
        passed through.
      '';
    };

    sslCertificates = {
      enable = mkEnableOption "SSL/TLS certificate support";
      path = mkOption {
        description = "SSL/TLS certificate bundle file";
        type = types.path;
        default = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";
      };
    };
  };

  config.bubblewrap = mkMerge [
    (
      let
        tz = config.etc.timezoneOverride;
      in
      mkIf (tz != "UTC") {
        # TODO: According to LOCALTIME(5), `/etc/localtime` should be a symlink
        # into `/etc/zoneinfo/` so that it provides the timezone name.  However
        # bind mount destinations are resolved.  We should read the destination
        # of `/etc/localtime` and recreate the symlink in the sandbox.
        bind.ro = [ "/etc/zoneinfo" ] ++ (lib.optional (tz == null) "/etc/localtime");
        symlinks = lib.optional (tz != null) [
          "/etc/localtime"
          "/etc/zoneinfo/${tz}"
        ];
      }
    )

    (
      let
        cfg = config.etc.sslCertificates;
      in
      mkIf cfg.enable {
        bind.ro = [
          [
            cfg.path
            "/etc/ssl/certs/ca-bundle.crt"
          ]
          [
            cfg.path
            "/etc/ssl/certs/ca-certificates.crt"
          ]
        ];
      }
    )
  ];
}
