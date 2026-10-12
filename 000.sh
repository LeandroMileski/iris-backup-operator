multipass launch 24.04 --name op-dev --cpus 4 --memory 8G --disk 40G \
  --cloud-init dev-vm.cloud-init.yaml
echo "cloud-init finished"
multipass exec op-dev -- cloud-init status --wait
echo "cloud-init status finished"

# kind cluster test
echo "Creating kind cluster"
kind create cluster --name smoke

# kubectl test
echo "Listing nodes"
kubectl get nodes

# kind cluster delete
echo "Deleting kind cluster"
kind delete cluster --name smoke

