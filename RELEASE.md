# Boskos Release Process
This document outlines the procedure for creating a new release for the Boskos project. Following these steps ensures that release tags are correctly associated with their corresponding container images and that those images are promoted to the official registry.

## Step 1: Tag the Release Commit
First, identify the commit you intend to release. Create a new semantic version tag for this commit, incrementing the patch version from the previous release.

Create the tag locally:

`git tag vX.Y.Z <your-commit-hash>`

(Replace `vX.Y.Z` with the new version number, e.g., `v0.0.1`)

Push the new tag to the repo:

`git push origin vX.Y.Z`

## Step 2: Verify Container Images
Next, confirm that all required container images for the tagged commit have been successfully built and pushed to the staging container registry, this is done by a postsubmit Prow job automatically.

Execute the `check_images.sh` script, providing the first 7 characters of your release commit's hash as an argument.

`./hack/check_images.sh <7-char-commit-hash>`

## Step 3: Review Verification Results
The outcome of the script determines the next action.

✅ On Success: If the script completes successfully, it will print the SHA digest for each container image to the console.

❌ On Failure: If the script fails, it means one or more images are not present in the staging repository. You need to investigate the cause of the build failure before proceeding.

> **Troubleshooting**: Visit the [post-boskos-push-images](https://prow.k8s.io/job-history/gs/kubernetes-ci-logs/logs/post-boskos-push-images) job history on Prow to find the failed build job and view its logs to diagnose the problem.

## Step 4: Update the Image Registry File
With the new version tag and image digests, you must update the central image manifest in the kubernetes/k8s.io repository.

Clone the [kubernetes/k8s.io](https://github.com/kubernetes/k8s.io/tree/main) repository if you don't have it locally.

Open the following file for editing: registry.k8s.io/images/k8s-staging-boskos/images.yaml

Add the image hashes to the file, associating them with the semver tag you created in Step 1.

Create a Pull Request with your changes to the kubernetes/k8s.io repository.

## Step 5: Wait for Image Promotion
After your Pull Request is reviewed and merged, the process is nearly complete.

The final step is to wait for the image promotion process, which is handled by [automation](https://github.com/kubernetes/k8s.io/tree/main/registry.k8s.io#image-promoter). Once the promotion is finished, the release is officially live.