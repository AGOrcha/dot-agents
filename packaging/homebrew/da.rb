# DRAFT homebrew-core formula for `da` (the dot-agents CLI).
#
# STAGING ONLY — not yet submitted to homebrew-core. See ./README.md.
# homebrew-core builds from source and then publishes bottles, so consumers get a
# prebuilt binary with no Go on their machine (`depends_on "go" => :build` is
# build-time only).
#
# Before submitting (after the AGOrcha cutover and once the project clears
# Homebrew's notability bar):
#   - set `url` to the tagged source tarball and fill `sha256`;
#   - confirm no existing core formula installs a `da` binary (add `conflicts_with`);
#   - run `brew audit --new --formula da` and `brew style da`.
class Da < Formula
  desc "Manage AI agent configurations across projects"
  homepage "https://github.com/AGOrcha/dot-agents"
  url "https://github.com/AGOrcha/dot-agents/archive/refs/tags/v0.3.4.tar.gz" # Follow-up: first post-cutover tag at submission
  sha256 "0000000000000000000000000000000000000000000000000000000000000000" # Follow-up: shasum -a 256 of the tarball
  license "MIT"
  head "https://github.com/AGOrcha/dot-agents.git", branch: "master"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/AGOrcha/dot-agents/commands.Version=#{version}
      -X github.com/AGOrcha/dot-agents/commands.Describe=v#{version}
    ]
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/da"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/da --version")
  end
end
