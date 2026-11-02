#!/bin/sh
# MCPcell installer for macOS and Linux: downloads a release, installs it into
# ~/.mcpcell/bin with the command-line interface, turns autostart on, takes
# over the agents' MCPs and shows the status. Meant for one line, run by a
# person or an AI agent:
#
#   curl -fsSL https://raw.githubusercontent.com/luohui1/MCP-cell/main/scripts/install.sh | sh
#
# Environment:
#   MCPCELL_VERSION=v0.6.1    a release tag instead of the latest
#   MCPCELL_NO_AUTOSTART=1    leave autostart as it is
#   MCPCELL_NO_TAKEOVER=1     install only, take nothing over
#   MCPCELL_HOME              MCPcell's data directory (default ~/.mcpcell)
set -eu

repo=luohui1/MCP-cell
fail() {
	echo "mcpcell: $*" >&2
	exit 1
}

case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "不支持这个系统：$(uname -s)（支持 macOS、Linux；Windows 用 install.ps1）" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "不支持这个架构：$(uname -m)" ;;
esac
if [ "$os" = linux ] && [ "$arch" != amd64 ]; then
	fail "Linux 只提供 x86_64 版本（当前：$(uname -m)）"
fi

pkg="mcpcell-$os-$arch"
if [ -n "${MCPCELL_VERSION:-}" ]; then
	url="https://github.com/$repo/releases/download/$MCPCELL_VERSION/$pkg.tar.gz"
else
	url="https://github.com/$repo/releases/latest/download/$pkg.tar.gz"
fi
mcpcell="${MCPCELL_HOME:-$HOME/.mcpcell}/bin/mcpcell"

# A new, empty directory for the download, removed on exit.
tmp=$(mktemp -d 2>/dev/null || mktemp -d -t mcpcell-install)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' INT TERM

echo "下载 $url"
if command -v curl >/dev/null 2>&1; then
	curl -fsSL "$url" -o "$tmp/$pkg.tar.gz" || fail "下载失败"
elif command -v wget >/dev/null 2>&1; then
	wget -q "$url" -O "$tmp/$pkg.tar.gz" || fail "下载失败"
else
	fail "需要 curl 或 wget"
fi
tar -xzf "$tmp/$pkg.tar.gz" -C "$tmp" || fail "解压失败"
if [ "$os" = darwin ]; then
	# The package is not signed; a browser download would be quarantined.
	xattr -dr com.apple.quarantine "$tmp/$pkg" 2>/dev/null || true
fi

# stdin is this script when it comes through a pipe: keep it from the
# programs.
"$tmp/$pkg/mcpcell" install --cli </dev/null || fail "安装失败"
if [ "${MCPCELL_NO_AUTOSTART:-}" != 1 ]; then
	"$mcpcell" autostart on </dev/null || echo "mcpcell: 没有开启开机自启" >&2
fi
if [ "${MCPCELL_NO_TAKEOVER:-}" != 1 ]; then
	"$mcpcell" takeover -y </dev/null || echo "mcpcell: 部分条目没有托管，原因见上面" >&2
fi
"$mcpcell" status </dev/null || true

echo
echo "MCPcell 已安装到 $(dirname "$mcpcell")。新开的 Agent 对话就会用它。"
echo "撤销：$mcpcell restore --all"
