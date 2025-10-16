## Steps

### Create the kubernetes cluster

```bash
minikube start -p walkai-dev \
--apiserver-ips=192.168.76.2,127.0.0.1 \
--apiserver-names=localhost,k8s.walkai.internal \
--driver=docker \
--container-runtime=docker  \
--cpus=22 \
--memory=200g  \
--gpus nvidia.com  \
--force
```

### Disable the default addon

```bash
minikube addons disable nvidia-device-plugin
kubectl -n kube-system delete ds nvidia-device-plugin-daemonset --ignore-not-found
```

### Install GPU Operator using helm

```bash
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia && helm repo update
helm install --wait --generate-name \
     -n gpu-operator --create-namespace \
     nvidia/gpu-operator --version v22.9.0 \
     --set driver.enabled=false \
     --set migManager.enabled=false \
     --set mig.strategy=mixed \
     --set toolkit.enabled=true
```

### Install cert-manager
```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.18.2/cert-manager.yaml
```

```bash
git clone https://github.com/walkai-org/walkai-nos.git
```
```bash
cd walkai-nos
```

```bash
kubectl label node walkai-dev nos.nebuly.com/gpu-partitioning=mig --overwrite
```
```bash
kubectl apply -k config/migagent/default
kubectl apply -k config/gpuagent/default
kubectl apply -k config/gpupartitioner/default
```

```bash
kubectl get pods -n nos-system -w
```
