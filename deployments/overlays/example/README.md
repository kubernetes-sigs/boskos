This overlay creates a fairly simple Boskos deployment in the namespace `boskos-example`.

It manages the following ficticious resources:
- `phonetic-project` (cleaned by the phonetic janitor)
- `numeric-project` (cleaned by the numeric janitor)
- `manual-token`
- `automatic-token` (automatically scaled using Dynamic Resource Lifecycles, though no Mason implementation is used here)

Beyond the core Boskos server, the following components are installed:
- The `cleaner`, to clean up `automatic-token`s
- The `reaper`, to clean up orphaned leases
- Two deployments of the `janitor`, one to clean up `numeric` projects, the other to clean up `phonetic` projects. Note that we increased the number of replicas for the latter.
  - In this toy example we overrode the real janitor binary to always pass, since these are not real projects and we do not have a real service account. A real implementation would need to ensure the janitor service account has the necessary credentials, for example by using [Workload Identity on GKE](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity).

To try out this example, you will need to [install Kustomize](https://kubernetes-sigs.github.io/kustomize/installation/). (The version of Kustomize included in Kubectl is too old at time of writing.)
Additionally, to play with this example locally, you can first create a [kind cluster](https://kind.sigs.k8s.io/).

Build the manifests:
```console
# from root of this repository; you can also run 'kustomize build .' from within this directory
$ kustomize build deployments/overlays/example/
```

Apply to a cluster:
```console
$ kustomize build deployments/overlays/example | kubectl apply -f-
```
