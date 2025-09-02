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

# GitHub Actions vars:
# $GITHUB_WORKSPACE   -> root of repo
# $GITHUB_REPOSITORY  -> owner/repo
# $GITHUB_REF_NAME    -> branch or tag name
# $GITHUB_SHA         -> current commit SHA
# $GITHUB_RUN_NUMBER  -> CI run number
# $GITHUB_EVENT_NAME  -> event type (push, pull_request, etc)
# $GITHUB_EVENT_PATH  -> path to the full webhook event JSON

mkdir -p "$GITHUB_WORKSPACE/gh-pages"
cd "$GITHUB_WORKSPACE/gh-pages" || exit 1

OLD_COVERAGE=0
NEW_COVERAGE=0
RESULT_MESSAGE=""

BADGE_COLOR=red
GREEN_THRESHOLD=85
YELLOW_THRESHOLD=50

# clone and prepare gh-pages branch
git clone -b gh-pages https://"$GHE_USER":"$GHE_TOKEN"@github.com/"$GITHUB_REPOSITORY".git .
git config user.name "travis"
git config user.email "travis"

mkdir -p "$GITHUB_WORKSPACE/gh-pages/coverage/$GITHUB_REF_NAME"
mkdir -p "$GITHUB_WORKSPACE/gh-pages/coverage/$GITHUB_SHA"

# calculate overall coverage percentage
echo "Computing the coverages"

if [ -f "$GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/cover.html" ]; then
    OLD_COVERAGE=$(grep "%)" "$GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/cover.html" \
      | sed 's/[][()><%]/ /g' | awk '{ print $4 }' \
      | awk '{s+=$1}END{if(NR>0)print s/NR; else print 0}')
fi

cp "$GITHUB_WORKSPACE/cover.html" "$GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}"
cp "$GITHUB_WORKSPACE/cover.html" "$GITHUB_WORKSPACE/gh-pages/coverage/$GITHUB_SHA"

if [ -f "$GITHUB_WORKSPACE/Passing" ]; then
    curl -s https://img.shields.io/badge/e2e-passing-yellow.svg > "$GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/e2e.svg"
elif [ -f "$GITHUB_WORKSPACE/Failed" ]; then
    curl -s https://img.shields.io/badge/e2e-failed-red.svg > "$GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/e2e.svg"
fi

NEW_COVERAGE=$(grep "%)" "$GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/cover.html" | sed 's/[][()><%]/ /g' | awk '{ print $4 }' \
  | awk '{s+=$1}END{if(NR>0)print s/NR; else print 0}')

# pick badge color
if (( $(echo "$NEW_COVERAGE > $GREEN_THRESHOLD" | bc -l) )); then
	BADGE_COLOR="green"
elif (( $(echo "$NEW_COVERAGE > $YELLOW_THRESHOLD" | bc -l) )); then
	BADGE_COLOR="yellow"
fi

# generate badge
curl -s https://img.shields.io/badge/coverage-"$NEW_COVERAGE"-$BADGE_COLOR.svg > "$GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/badge.svg"

# generate result message
if (( $(echo "$OLD_COVERAGE > $NEW_COVERAGE" | bc -l) )); then
	RESULT_MESSAGE=":red_circle: Coverage decreased from [$OLD_COVERAGE%] to [$NEW_COVERAGE%]"
elif (( $(echo "$OLD_COVERAGE == $NEW_COVERAGE" | bc -l) )); then
	RESULT_MESSAGE=":thumbsup: Coverage remained same at [$NEW_COVERAGE%]"
else
	RESULT_MESSAGE=":thumbsup: Coverage increased from [$OLD_COVERAGE%] to [$NEW_COVERAGE%]"
fi

# update gh-pages or PR
echo "Updating gh-pages"
if [ "$GITHUB_EVENT_NAME" == "push" ]; then
	git status
	git add .
	git commit -m "Coverage result for commit $GITHUB_SHA from run $GITHUB_RUN_NUMBER" || echo "No changes to commit"
	git push origin gh-pages
elif [ "$GITHUB_EVENT_NAME" == "pull_request" ]; then
	# extract PR number from JSON
	PR_NUMBER=$(jq -r .pull_request.number "$GITHUB_EVENT_PATH")

	curl -s -X POST \
	  -H "Authorization: token $GHE_TOKEN" \
	  -H "Content-Type: application/json" \
	  -d "{\"body\": \"$RESULT_MESSAGE\"}" \
	  "https://api.github.com/repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments"
fi
