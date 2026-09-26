class AgentsSandbox < Formula
  desc "Run coding agents in disposable microsandbox VMs"
  homepage "https://inoio.github.io/agents-sandbox/"
  license "GPL-3.0-or-later"

  livecheck do
    url :stable
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/inoio/agents-sandbox/releases/download/v0.4.0/agents-sandbox-darwin-arm64"
      sha256 "b3b947cb44b8764ac1fef0b0aed46fe347bf1ac39295cbeba25724de9479d995"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/inoio/agents-sandbox/releases/download/v0.4.0/agents-sandbox-linux-amd64"
      sha256 "572ca199f0a59d5ecf238db8738c2177e01a501a1cd7aa61e9eba04fca2d42b2"
    end

    on_arm do
      url "https://github.com/inoio/agents-sandbox/releases/download/v0.4.0/agents-sandbox-linux-arm64"
      sha256 "d47628849f44d7ca8f22e0fe56997da0bcf1291083caf18c3aac8c67d089f551"
    end
  end

  def install
    binary = Pathname.glob("agents-sandbox-*").first
    chmod 0755, binary
    bin.install binary => "agents-sandbox"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/agents-sandbox version")
  end
end
