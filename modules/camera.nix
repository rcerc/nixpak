{ config, lib, ... }:

{
  options.camera.enable = lib.mkEnableOption "incomplete and hacky camera access";

  config.bubblewrap.bind.dev = lib.mkIf config.camera.enable [
    "/dev/v4l"
    "/dev/video0"
    "/dev/video1"
    "/dev/video2"
    "/dev/video3"
    "/dev/video4"
    "/dev/video5"
    "/dev/video6"
  ];
}
