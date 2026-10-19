#!/usr/bin/env bash
# Install the VolumeSnapshot CRDs, the snapshot controller, and the CSI hostpath
# driver so an e2e BackupPolicy can snapshot a real volume.
set -euo pipefail

SNAPSHOTTER_REF=v8.6.0
HOSTPATH_REF=v1.18.0

kubectl apply -k "https://github.com/kubernetes-csi/external-snapshotter/client/config/crd?ref=${SNAPSHOTTER_REF}"
kubectl wait --for=condition=established --timeout=120s \
	crd/volumesnapshots.snapshot.storage.k8s.io \
	crd/volumesnapshotclasses.snapshot.storage.k8s.io
kubectl apply -k "https://github.com/kubernetes-csi/external-snapshotter/deploy/kubernetes/snapshot-controller?ref=${SNAPSHOTTER_REF}"
kubectl -n kube-system rollout status deploy/snapshot-controller --timeout=180s

src="$(mktemp -d)"
trap 'rm -rf "$src"' EXIT
git clone --depth 1 --branch "${HOSTPATH_REF}" https://github.com/kubernetes-csi/csi-driver-host-path.git "${src}/driver"

# deploy-hostpath.sh finds its manifests next to $0. Copy it into the version
# directory, which is where hostpath/*.yaml lives. kubernetes-1.34/deploy.sh is
# only a pointer at that script.
cp "${src}/driver/deploy/util/deploy-hostpath.sh" "${src}/driver/deploy/kubernetes-1.34/run-deploy.sh"
bash "${src}/driver/deploy/kubernetes-1.34/run-deploy.sh"
kubectl apply -f "${src}/driver/examples/csi-storageclass.yaml"
kubectl apply -f "${src}/driver/examples/csi-volumesnapshotclass.yaml"
