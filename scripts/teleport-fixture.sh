#!/usr/bin/env bash
# A local Teleport proxy in front of a kind cluster, for the live checks of the Teleport
# sign-in (go/ichorgo/kube_auth_teleport_live_test.go). See docs/build.md "Teleport fixture".
#
#   scripts/teleport-fixture.sh up     # or: just teleport-up
#   scripts/teleport-fixture.sh down   # or: just teleport-down
#
# Needs Docker (or Podman), kind and kubectl. Writes only under .teleport/ (git-ignored).
# The SSO console sign-in is not covered: Community Edition's GitHub connector cannot point
# at a fake, so that stays a hand check against a real proxy.
set -euo pipefail

TELEPORT_VERSION=18.11.10
IMAGE="public.ecr.aws/gravitational/teleport-distroless:${TELEPORT_VERSION}"
KIND_CLUSTER=ichor-teleport
CONTAINER=ichor-teleport
KUBE_CLUSTER=sample-kube
PROXY=localhost:3080

root=$(git rev-parse --show-toplevel)
out="$root/.teleport"
here="$root/scripts/teleport"

# docker, or podman when only Podman is there (kind then runs on Podman too).
if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
  engine=docker
elif command -v podman >/dev/null; then
  engine=podman
  export KIND_EXPERIMENTAL_PROVIDER=podman
else
  echo "teleport-fixture: needs Docker or Podman" >&2
  exit 1
fi

for tool in kind kubectl; do
  command -v "$tool" >/dev/null || { echo "teleport-fixture: needs $tool on PATH" >&2; exit 1; }
done

tctl() { "$engine" exec "$CONTAINER" tctl "$@"; }

# wait_for <what> <seconds> <command…>: retries the command once a second.
wait_for() {
  local what=$1 seconds=$2
  shift 2
  for _ in $(seq "$seconds"); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "teleport-fixture: $what not ready after ${seconds}s" >&2
  "$engine" logs --tail 30 "$CONTAINER" >&2 || true
  exit 1
}

kube_listed() { tctl kube ls --format=text | grep -q "$KUBE_CLUSTER"; }

# sign <user> <ttl> <file>: a certificate kubeconfig for user, as tsh would get after a sign-in.
sign() {
  local err
  # Quiet on success: tctl prints a product tip on stderr every time.
  if ! err=$(tctl auth sign --format=kubernetes --user="$1" --ttl="$2" --proxy="$PROXY" \
    --kube-cluster-name="$KUBE_CLUSTER" --out="/var/lib/teleport/$3" --overwrite 2>&1); then
    echo "$err" >&2
    exit 1
  fi
  # Copied out rather than written through the mount, so the files are the caller's to delete.
  "$engine" cp "$CONTAINER:/var/lib/teleport/$3" "$out/$3"
}

# tsh_kubeconfig: live.kubeconfig with the exec: tsh user `tsh kube login --insecure` writes
# (the proxy's web certificate is self-signed), for
# `just probe kubeconfig` and the import preview (tsh need not be installed: it is parsed only).
tsh_kubeconfig() {
  local cfg="$out/tsh.kubeconfig" user
  cp "$out/live.kubeconfig" "$cfg"
  user=$(kubectl --kubeconfig "$cfg" config view -o jsonpath='{.contexts[0].context.user}')
  kubectl --kubeconfig "$cfg" config unset "users.$user.client-certificate-data" >/dev/null
  kubectl --kubeconfig "$cfg" config unset "users.$user.client-key-data" >/dev/null
  kubectl --kubeconfig "$cfg" config set-credentials "$user" \
    --exec-api-version=client.authentication.k8s.io/v1beta1 --exec-command=tsh \
    --exec-arg=kube --exec-arg=credentials --exec-arg="--kube-cluster=$KUBE_CLUSTER" \
    --exec-arg=--teleport-cluster=teleport.local --exec-arg="--proxy=$PROXY" \
    --exec-arg=--insecure >/dev/null
}

up() {
  local start=$SECONDS
  mkdir -p "$out"

  # A cluster stopped by a reboot does not come back cleanly: start it over.
  if [ "$("$engine" inspect -f '{{.State.Running}}' "$KIND_CLUSTER-control-plane" 2>/dev/null)" = false ]; then
    kind delete cluster --name "$KIND_CLUSTER"
  fi

  if ! kind get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER"; then
    kind create cluster --name "$KIND_CLUSTER" --wait 180s
  fi

  # The image starts Teleport with /etc/teleport/teleport.yaml.
  # The kind API server as the Teleport container sees it: by name on the kind network.
  kind get kubeconfig --internal --name "$KIND_CLUSTER" >"$out/kind.kubeconfig"
  kubectl --kubeconfig "$out/kind.kubeconfig" config rename-context "kind-$KIND_CLUSTER" "$KUBE_CLUSTER" >/dev/null
  cp "$here/teleport.yaml" "$here/roles.yaml" "$out/"

  if [ "$("$engine" inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null)" != true ]; then
    "$engine" rm -f "$CONTAINER" >/dev/null 2>&1 || true
    "$engine" run -d --name "$CONTAINER" --network kind --hostname teleport.local \
      -p 127.0.0.1:3080:3080 -v "$out:/etc/teleport:ro,Z" \
      "$IMAGE" >/dev/null
  fi

  wait_for "Teleport auth" 90 tctl status
  tctl create -f /etc/teleport/roles.yaml >/dev/null 2>&1 || tctl create -f --force /etc/teleport/roles.yaml >/dev/null
  tctl get users/live >/dev/null 2>&1 || tctl users add live --roles=kube-live >/dev/null
  tctl get users/live-mfa >/dev/null 2>&1 || tctl users add live-mfa --roles=kube-mfa >/dev/null
  wait_for "the $KUBE_CLUSTER registration" 90 kube_listed

  sign live 12h live.kubeconfig
  sign live 1m live-ttl.kubeconfig
  sign live-mfa 12h live-mfa.kubeconfig
  tsh_kubeconfig

  cat <<EOF
Teleport ${TELEPORT_VERSION} at https://${PROXY}, kind cluster "${KUBE_CLUSTER}" ($((SECONDS - start))s).
export TELEPORT_LIVE_KUBECONFIG=$out/live.kubeconfig
export TELEPORT_LIVE_TTL_KUBECONFIG=$out/live-ttl.kubeconfig
export TELEPORT_LIVE_MFA_KUBECONFIG=$out/live-mfa.kubeconfig
cd go && go test ./ichorgo -run TestTeleportLive -count=1 -v
A tsh kubeconfig to import by hand: $out/tsh.kubeconfig
EOF
}

down() {
  "$engine" rm -f "$CONTAINER" >/dev/null 2>&1 || true
  kind delete cluster --name "$KIND_CLUSTER" >/dev/null 2>&1 || true
  rm -rf "$out"
  echo "Teleport fixture removed."
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  *) echo "usage: $0 up|down" >&2; exit 2 ;;
esac
