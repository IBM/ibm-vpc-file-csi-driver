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

mkdir -p "$GITHUB_WORKSPACE/gh-pages"
cd "$GITHUB_WORKSPACE/gh-pages" || return

OLD_COVERAGE=0
NEW_COVERAGE=0
RESULT_MESSAGE=""

BADGE_COLOR=red
GREEN_THRESHOLD=85
YELLOW_THRESHOLD=50

# clone and prepare gh-pages branch
git clone -b gh-pages https://$GHE_USER:$GHE_TOKEN@github.com/$GITHUB_REPOSITORY.git .
git config user.name "github-actions"
git config user.email "github-actions@github.com"

if [ ! -d "$GITHUB_WORKSPACE/gh-pages/coverage" ]; then
    mkdir "$GITHUB_WORKSPACE/gh-pages/coverage"
fi

if [ ! -d "$GITHUB_WORKSPACE/gh-pages/coverage/$GITHUB_REF_NAME" ]; then
    mkdir "$GITHUB_WORKSPACE/gh-pages/coverage/$GITHUB_REF_NAME"
fi

if [ ! -d "$GITHUB_WORKSPACE/gh-pages/coverage/$GITHUB_SHA" ]; then
	mkdir "$GITHUB_WORKSPACE/gh-pages/coverage/$GITHUB_SHA"
fi

if [ -f "$(go env GOPATH)/src/github.com/IBM/ibm-vpc-file-csi-driver/Passing" ]; then
	curl https://img.shields.io/badge/e2e-passing-Yellow.svg > $GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/e2e.svg
elif [ -f "$GOPATH/src/github.com/IBM/ibm-vpc-file-csi-driver/Failed" ]; then
	curl https://img.shields.io/badge/e2e-failed-Yellow.svg > $GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/e2e.svg
fi

# Compute overall coverage percentage
echo "Computing the coverages"
OLD_COVERAGE=$(cat $GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/cover.html  | grep "%)"  | sed 's/[][()><%]/ /g' | awk '{ print $4 }' | awk '{s+=$1}END{print s/NR}')
cp $GITHUB_WORKSPACE/cover.html $GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}
cp $GITHUB_WORKSPACE/cover.html $GITHUB_WORKSPACE/gh-pages/coverage/$TRAVIS_COMMIT
NEW_COVERAGE=$(cat $GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/cover.html  | grep "%)"  | sed 's/[][()><%]/ /g' | awk '{ print $4 }' | awk '{s+=$1}END{print s/NR}')

if (( $(echo "$NEW_COVERAGE > $GREEN_THRESHOLD" | bc -l) )); then
	BADGE_COLOR="green"
elif (( $(echo "$NEW_COVERAGE > $YELLOW_THRESHOLD" | bc -l) )); then
	BADGE_COLOR="yellow"
fi

# Generate badge for coverage
curl https://img.shields.io/badge/coverage-$NEW_COVERAGE-$BADGE_COLOR.svg > $GITHUB_WORKSPACE/gh-pages/coverage/${GITHUB_REF_NAME}/badge.svg

COMMIT_RANGE=(${TRAVIS_COMMIT_RANGE//.../ })

# Generate result message for log and PR
if (( $(echo "$OLD_COVERAGE > $NEW_COVERAGE" | bc -l) )); then
	RESULT_MESSAGE=":red_circle: Coverage decreased from [$OLD_COVERAGE%] to [$NEW_COVERAGE%]"
elif (( $(echo "$OLD_COVERAGE == $NEW_COVERAGE" | bc -l) )); then
	RESULT_MESSAGE=":thumbsup: Coverage remained same at [$NEW_COVERAGE%]"
else
	RESULT_MESSAGE=":thumbsup: Coverage increased from [$OLD_COVERAGE%] to [$NEW_COVERAGE%]"
fi

# Update gh-pages branch or PR
echo "Updating gh-pages"
if [ "$TRAVIS_PULL_REQUEST" == "false" ]; then
	git status
	git add .
	git commit -m "Coverage result for commit $TRAVIS_COMMIT from build $TRAVIS_BUILD_NUMBER"
	git push origin
else
        # Updates PR with coverage
   		curl -X POST -H "Authorization: Token $GHE_TOKEN" "https://api.github.com/repos/$GITHUB_REPOSITORY/issues/$TRAVIS_PULL_REQUEST/comments" -H 'Content-Type: application/json' --data '{"body": "'"$RESULT_MESSAGE"'"}'

fi
