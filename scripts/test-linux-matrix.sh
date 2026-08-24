#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
MATRIX=${SENTINEL_LINUX_MATRIX:-"ubuntu:24.04@sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517 debian:12-slim@sha256:abd67ffcfa541b485a3dff59865ab629aa048a6c613e639d36e7456b0b229241 rockylinux:9-minimal@sha256:305de618a5681ff75b1d608fd22b10f362867dff2f550a4f1d427d21cd7f42b4 fedora:42@sha256:99e203b80b1c3d8f7e161ec10a68fd02b081ef83a3963553e513c82846b97814 opensuse/leap:15.6@sha256:79be7751205ea84559990fb76b1bec71e38d6fad41c70a4f6c921b803b58f421 alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659"}

for image in $MATRIX; do
  echo "Validando preflight em $image"
  docker run --rm -e SENTINEL_MIN_MEMORY_GIB=1 -e SENTINEL_MIN_DISK_GIB=1 \
    -v "$ROOT:/workspace:ro" -w /workspace "$image" /bin/sh -ec '
      ./scripts/install-linux-server.sh --phase preflight
      ./scripts/install-linux-collector.sh --phase preflight --host-name matrix-host
      ./scripts/install-linux-server.sh --help >/dev/null
      ./scripts/install-linux-collector.sh --help >/dev/null
      ./bootstrap-linux.sh --help >/dev/null
      . ./scripts/lib/linux-common.sh
      manager=$(detect_package_manager)
      case "$manager" in apt-get|dnf|yum|zypper|apk|pacman|microdnf) ;; *) exit 1 ;; esac
    '
done

echo "Matriz Linux concluída com sucesso."
