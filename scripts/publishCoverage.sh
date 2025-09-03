#!/bin/bash

#/******************************************************************************
 #Copyright 2022 IBM Corp.
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
# *****************************************************************************/
set -x
echo "Publishing the coverage results"

# GitHub Actions env vars:
# $GITHUB_WORKSPACE   -> repo root on runner
# $GITHUB_REPOSITORY  -> owner/repo
# $GITHUB_REF_NAME    -> branch or tag name
# $GITHUB_BASE_REF    -> base branch for PRs (e.g. master)
# $GITHUB_SHA         -> commit SHA
# $GITHUB_RUN_NUMBER  -> workflow run number
# $GITHUB_EVENT_NAME  -> event type (push, pull_request, etc)
# $GITHUB_EVENT_PATH  -> webhook event JSON

WORKDIR="$GITHUB_WORKSPACE/gh-pages"
NEW_COVERAGE_SOURCE="$GITHUB_WORKSPACE/cover.html"
OLD_COVERAGE=0
NEW_COVERAGE=0
RESULT_MESSAGE=""
BADGE_COLOR="red"
GREEN_THRESHOLD=85
YELLOW_THRESHOLD=50

# Decide which branch to compare against
if [[ "$GITHUB_EVENT_NAME" == "pull_request" ]]; then
    BASE_BRANCH="$GITHUB_BASE_REF"
else
    BASE_BRANCH="$GITHUB_REF_NAME"
fi

# Compute new coverage FIRST
if [ -f "$NEW_COVERAGE_SOURCE" ]; then
    NEW_COVERAGE=$(grep "%)" "$NEW_COVERAGE_SOURCE" \
      | sed 's/[][()><%]/ /g' \
      | awk '{s+=$4}END{if(NR>0)print s/NR; else print 0}')
else
    echo "New coverage report not found at $NEW_COVERAGE_SOURCE"
    exit 1
fi

# Prepare the gh-pages directory
mkdir -p "$WORKDIR"
cd "$WORKDIR" || exit 1

# clone gh-pages branch
git clone -b gh-pages "https://$GHE_USER:$GHE_TOKEN@github.com/$GITHUB_REPOSITORY.git" .
git config user.name "github actions"
git config user.email "actions@github.com"

# define the path to the OLD coverage report that was just cloned
COVERAGE_DIR="coverage/$BASE_BRANCH"
OLD_COVER_HTML="$COVERAGE_DIR/cover.html"

# compute old coverage
OLD_COVERAGE=$(grep "%)" "$OLD_COVER_HTML" 2>/dev/null \
  | sed 's/[][()><%]/ /g' \
  | awk '{s+=$4}END{if(NR>0)print s/NR; else print 0}')

# Round coverage to 2 decimal places
OLD_COVERAGE=$(printf "%.2f" "$OLD_COVERAGE")
NEW_COVERAGE=$(printf "%.2f" "$NEW_COVERAGE")

echo "Old Coverage: $OLD_COVERAGE%"
echo "New Coverage: $NEW_COVERAGE%"

# copy the new report over to update gh-pages branch
mkdir -p "$COVERAGE_DIR"
mkdir -p "coverage/$GITHUB_SHA"
cp "$NEW_COVERAGE_SOURCE" "$COVERAGE_DIR/cover.html"
cp "$NEW_COVERAGE_SOURCE" "coverage/$GITHUB_SHA/cover.html"

# Decide badge color
if (( $(echo "$NEW_COVERAGE > $GREEN_THRESHOLD" | bc -l) )); then
    BADGE_COLOR="green"
elif (( $(echo "$NEW_COVERAGE > $YELLOW_THRESHOLD" | bc -l) )); then
    BADGE_COLOR="yellow"
fi

curl -s "https://img.shields.io/badge/coverage-${NEW_COVERAGE}%25-${BADGE_COLOR}.svg" > "$COVERAGE_DIR/badge.svg"

# Build result message
if (( $(echo "$OLD_COVERAGE > $NEW_COVERAGE" | bc -l) )); then
    RESULT_MESSAGE=":red_circle: Coverage decreased from [$OLD_COVERAGE%] to [$NEW_COVERAGE%]"
elif (( $(echo "$OLD_COVERAGE == $NEW_COVERAGE" | bc -l) )); then
    RESULT_MESSAGE=":thumbsup: Coverage remained the same at [$NEW_COVERAGE%]"
else
    RESULT_MESSAGE=":thumbsup: Coverage increased from [$OLD_COVERAGE%] to [$NEW_COVERAGE%]"
fi

# Update gh-pages branch or PR
if [[ "$GITHUB_EVENT_NAME" == "push" ]]; then
    git add .
    git commit -m "Coverage: commit $GITHUB_SHA (run $GITHUB_RUN_NUMBER)" || echo "No changes to commit"
    git push "https://github-actions:${GHE_TOKEN}@github.com/${GITHUB_REPOSITORY}.git" gh-pages
elif [[ "$GITHUB_EVENT_NAME" == "pull_request" ]]; then
    PR_NUMBER=$(jq -r .pull_request.number "$GITHUB_EVENT_PATH")
    curl -s -X POST \
      -H "Authorization: token $GHE_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"body\": \"$RESULT_MESSAGE\"}" \
      "https://api.github.com/repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments"
fi

echo "Coverage script finished."