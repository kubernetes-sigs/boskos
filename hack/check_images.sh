#!/usr/bin/env bash
# Copyright 2025 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

REPO="gcr.io/k8s-staging-boskos"

# Check if a commit SHA was provided as a command-line argument
if [ -z "$1" ]; then
  echo "Usage: $0 <last_7_chars_commit_sha>"
  echo "Please provide the last 7 chars of the commit hash to search for in the image tags."
  exit 1
fi

COMMIT_SHA="$1"

# Get a list of all images in the specified repository
images=$(gcloud container images list --repository="$REPO" --format="get(name)")
expected_count=$(echo $images | wc -w | tr -d ' ')
echo "There are $expected_count distinct Boskos images in total"

# This should not happen.
if [ -z "$images" ]; then
  echo "No images found in repository: $REPO"
  exit 1
fi

echo "Searching for tags containing '$COMMIT_SHA' in repository '$REPO'..."
echo "---"

matched_count=0

printf "%-70s %s\n" "IMAGE_NAME" "MATCHED_IMAGE_SHA256"
for image in $images; do
  # Find the images whose tags match the commit hash
  digest=$(gcloud container images list-tags "$image" --filter="tags~$COMMIT_SHA" --format="get(digest)")

  # If any tags matched, add the image to our list of matches
  if [ -n "$digest" ]; then
    printf "%-70s %s\n" "$image" "$digest"
    ((matched_count += 1))
  fi
done

echo "Validation completed."
echo "Found $matched_count images with matching tags."

if [ "$matched_count" -eq "$expected_count" ]; then
  echo "✅ Success: The number of matched images is exactly $expected_count."
  exit 0
else
  echo "❌ Failure: Expected $expected_count images, but found $matched_count."
  exit 1
fi
