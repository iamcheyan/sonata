# Sonata packaged as a ROCKNIX application. Playback and decoding use mpv.
PKG_NAME="sonata"
PKG_VERSION="0.1.0"
PKG_LICENSE="MIT"
PKG_SITE="https://github.com/iamcheyan/sonata"
PKG_URL="${PKG_SITE}/archive/refs/tags/v${PKG_VERSION}.tar.gz"
PKG_DEPENDS_TARGET="toolchain mpv"
PKG_LONGDESC="Sonata local music library and mpv playback controller"
PKG_TOOLCHAIN="go"
PKG_GO_VERSION="1.21"

makeinstall_target() {
  mkdir -p ${INSTALL}/usr/bin
  install -m 0755 sonata ${INSTALL}/usr/bin/sonata
}
