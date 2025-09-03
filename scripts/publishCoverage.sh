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
# $GITHUB_REF_NAME    -> branch or tag name (e.g. master)
# $GITHUB_SHA         -> commit SHA
# $GITHUB_RUN_NUMBER  -> workflow run number
# $GITHUB_EVENT_NAME  -> event type (push, pull_request, etc)
# $GITHUB_EVENT_PATH  -> webhook event JSON

WORKDIR="$GITHUB_WORKSPACE/gh-pages"
COVERAGE_DIR="$WORKDIR/coverage/$GITHUB_REF_NAME"

mkdir -p "$WORKDIR"
cd "$WORKDIR" || exit 1

OLD_COVERAGE=0
NEW_COVERAGE=0
RESULT_MESSAGE=""

BADGE_COLOR=red
GREEN_THRESHOLD=85
YELLOW_THRESHOLD=50

# clone gh-pages branch
git clone -b gh-pages "https://$GHE_USER:$GHE_TOKEN@github.com/$GITHUB_REPOSITORY.git" .
git config user.name "travis"
git config user.email "travis"

echo "$WORKDIR"
echo "$GITHUB_REF_NAME"

mkdir -p "$COVERAGE_DIR" "$WORKDIR/coverage/$GITHUB_SHA"

# compute old coverage if present
COVER_HTML="$COVERAGE_DIR/cover.html"
OLD_COVERAGE=$(grep "%)" "$COVER_HTML" 2>/dev/null \
  | sed 's/[][()><%]/ /g' \
  | awk '{s+=$4}END{if(NR>0)print s/NR; else print 0}')

# copy new report
cp "$GITHUB_WORKSPACE/cover.html" "$COVERAGE_DIR"
cp "$GITHUB_WORKSPACE/cover.html" "$WORKDIR/coverage/$GITHUB_SHA"

# add e2e badge if test markers exist
if [[ -f "$GITHUB_WORKSPACE/Passing" ]]; then
    curl -s https://img.shields.io/badge/e2e-passing-green.svg > "$COVERAGE_DIR/e2e.svg"
elif [[ -f "$GITHUB_WORKSPACE/Failed" ]]; then
    curl -s https://img.shields.io/badge/e2e-failed-red.svg > "$COVERAGE_DIR/e2e.svg"
fi

# compute new coverage
NEW_COVERAGE=$(grep "%)" "$COVER_HTML" \
  | sed 's/[][()><%]/ /g' \
  | awk '{s+=$4}END{if(NR>0)print s/NR; else print 0}')

# select badge color
if (( $(echo "$NEW_COVERAGE > $GREEN_THRESHOLD" | bc -l) )); then
    BADGE_COLOR="green"
elif (( $(echo "$NEW_COVERAGE > $YELLOW_THRESHOLD" | bc -l) )); then
    BADGE_COLOR="yellow"
fi

# generate coverage badge
curl -s "https://img.shields.io/badge/coverage-${NEW_COVERAGE}-${BADGE_COLOR}.svg" > "$COVERAGE_DIR/badge.svg"

# build result message
if (( $(echo "$OLD_COVERAGE > $NEW_COVERAGE" | bc -l) )); then
    RESULT_MESSAGE=":red_circle: Coverage decreased from [$OLD_COVERAGE%] to [$NEW_COVERAGE%]"
elif (( $(echo "$OLD_COVERAGE == $NEW_COVERAGE" | bc -l) )); then
    RESULT_MESSAGE=":thumbsup: Coverage remained the same at [$NEW_COVERAGE%]"
else
    RESULT_MESSAGE=":thumbsup: Coverage increased from [$OLD_COVERAGE%] to [$NEW_COVERAGE%]"
fi

# updating comment on PR
echo "Updating gh-pages/PR comment"
if [[ "$GITHUB_EVENT_NAME" == "push" ]]; then
    git add .
    git commit -m "Coverage: commit $GITHUB_SHA (run $GITHUB_RUN_NUMBER)" || echo "No changes to commit"
    git push origin gh-pages
elif [[ "$GITHUB_EVENT_NAME" == "pull_request" ]]; then
    PR_NUMBER=$(jq -r .pull_request.number "$GITHUB_EVENT_PATH")
    curl -s -X POST \
      -H "Authorization: token $GHE_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"body\": \"$RESULT_MESSAGE\"}" \
      "https://api.github.com/repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments"
fi
