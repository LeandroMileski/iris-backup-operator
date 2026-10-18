multipass launch 24.04 --name op-dev --cpus 4 --memory 8G --disk 40G \
  --cloud-init dev-vm.cloud-init.yaml
echo "cloud-init finished"
multipass exec op-dev -- cloud-init status --wait
echo "cloud-init status finished"
echo "Creating kind cluster"
multipass exec op-dev -- bash -lc 'kind create cluster --name iris-dev --image kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5 --wait 5m'
echo "Kind cluster created"
echo "Applying snapshotter CRDs"
multipass exec op-dev -- bash -lc 'kubectl apply -k "https://github.com/kubernetes-csi/external-snapshotter/client/config/crd?ref=v8.6.0"'
multipass exec op-dev -- bash -lc 'kubectl apply -k "https://github.com/kubernetes-csi/external-snapshotter/deploy/kubernetes/snapshot-controller?ref=v8.6.0"'
multipass exec op-dev -- bash -lc 'kubectl -n kube-system rollout status deploy/snapshot-controller --timeout=180s'
echo "Snapshotter CRDs applied"

multipass exec op-dev -- bash -lc 'git clone --depth 1 https://github.com/kubernetes-csi/csi-driver-host-path.git ~/csi-driver-host-path && ~/csi-driver-host-path/deploy/kubernetes-latest/deploy.sh'
echo "CSI driver host path deployed"
multipass exec op-dev -- bash -lc 'cd ~/csi-driver-host-path && kubectl apply -f examples/csi-storageclass.yaml -f examples/csi-pvc.yaml && kubectl wait --for=jsonpath="{.status.phase}"=Bound pvc/csi-pvc --timeout=180s && kubectl apply -f examples/csi-snapshot-v1.yaml && kubectl wait --for=jsonpath="{.status.readyToUse}"=true volumesnapshot/new-snapshot-demo --timeout=180s && kubectl get volumesnapshot new-snapshot-demo'
echo "CSI driver host path test completed"

multipass mount /Users/leo/kube-projects/k8s-operator-kubebuilder-go op-dev:/home/ubuntu/iris-backup-operator
echo "Mounted iris-backup-operator to op-dev"
multipass exec op-dev -- bash -lc 'kubebuilder init \
  --domain leandro.ie \
  --repo github.com/LeandroMileski/iris-backup-operator \
  --project-name iris-backup-operator'
echo "Kubebuilder initialized"


