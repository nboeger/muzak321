class Muzak321 < Formula
  desc "Command-line music player (MP3, FLAC, OGG, WAV) with file browser and playlist"
  homepage "https://github.com/nboeger/muzak321"
  url "https://github.com/nboeger/muzak321/archive/refs/tags/0.1.16.tar.gz"
  sha256 "PLACEHOLDER"
  license "GPL-3.0-only"

  # Tags are pushed without a "v" prefix (e.g. 0.1.14), so the default
  # GitHub tag regex (which expects vX.Y.Z) cannot pick up new releases.
  livecheck do
    url :stable
    regex(/^(\d+(?:\.\d+)+)$/i)
  end

  depends_on "go" => :build

  on_linux do
    depends_on "alsa-lib"
  end

  def install
    system "go", "build", *std_go_args(ldflags: "-X main.version=#{version}")
  end

  test do
    assert_match "muzak321", shell_output("#{bin}/muzak321 --version")
  end
end
