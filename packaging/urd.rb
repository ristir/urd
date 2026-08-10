class Urd < Formula
  # A template rendered by packaging/formula.sh, which refuses to emit a formula with
  # a placeholder left in it. Valid Ruby as it stands, so a syntax check still works.
  #
  # No trailing period and not starting with the formula name: brew audit rejects both.
  desc "History search for zsh that rewrites your prompt"
  homepage "https://github.com/ristir/urd"
  license "MIT"
  version "@@VERSION@@"

  on_macos do
    if Hardware::CPU.arm?
      url "@@URL_DARWIN_ARM64@@"
      sha256 "@@SHA_DARWIN_ARM64@@"
    else
      url "@@URL_DARWIN_AMD64@@"
      sha256 "@@SHA_DARWIN_AMD64@@"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "@@URL_LINUX_ARM64@@"
      sha256 "@@SHA_LINUX_ARM64@@"
    else
      url "@@URL_LINUX_AMD64@@"
      sha256 "@@SHA_LINUX_AMD64@@"
    end
  end

  def install
    bin.install "urd"
  end

  def caveats
    <<~EOS
      One manual step left:

        urd --setup

      It shows the hook line and asks before adding it to your ~/.zshrc. Then
      restart your shell tab, or run: source ~/.zshrc

      By hand it is one line, at the very end of the file:

        eval "$(#{opt_bin}/urd hook zsh)"

      The full path is deliberate: a bare `urd` binds nothing at all in any shell
      that reads the rc file without the install directory on $PATH.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/urd --version")
  end
end
