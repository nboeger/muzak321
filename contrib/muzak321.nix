# muzak321 — Nixpkgs expression
#
# Draft for nixpkgs submission (pkgs/by-name/mu/muzak321/package.nix).
# Build locally without touching nixpkgs:
#
#   nix-build -E 'with import <nixpkgs> {}; callPackage ./contrib/muzak321.nix {}'
#
# First build will fail at the vendorHash check and print the real hash;
# paste it below and rebuild. Then verify with:
#
#   nix-build ... && result/bin/muzak321 --version
#
# Audio note: oto (the playback backend) locates ALSA via pkg-config
# (#cgo pkg-config: alsa), hence alsa-lib + pkg-config below. Linux-only
# by design (no Windows/BSD audio backend).

{ lib
, buildGoModule
, fetchFromGitHub
, alsa-lib
, pkg-config
}:

buildGoModule rec {
  pname = "muzak321";
  version = "0.1.16";

  src = fetchFromGitHub {
    owner = "nboeger";
    repo = "muzak321";
    rev = version; # tags have no "v" prefix
    hash = "sha256-/docLSOMF/6EJWP2uuuKWVAj3x1F3wf70/8LiiuB018=";
  };

  vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="; # TODO: fill from first build

  nativeBuildInputs = [ pkg-config ];
  buildInputs = [ alsa-lib ];

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];

  meta = with lib; {
    description = "Command-line music player (MP3, FLAC, OGG, WAV) with a file browser, progress bar, and playlist";
    homepage = "https://github.com/nboeger/muzak321";
    license = licenses.gpl3Only;
    mainProgram = "muzak321";
    platforms = platforms.linux;
  };
}
