#!/bin/bash
set -eu

echo "This is a test script for CV Platform."
echo ""

if [ -z "$DEVENV_ROOT" ] || [ ! -d "$DEVENV_ROOT" ]; then
  echo "DEVENV_ROOT is not set or not valid."
  echo "Did you started this script inside a devenv shell?"
  exit 1
fi

go -C services/uploader test -v
go -C services/extractor test -v
go -C services/piistripper test -v
go -C services/skillmatcher test -v
go -C services/offermatcher test -v
go -C services/aiweighter test -v
go -C services/roleinsights test -v
go -C services/datacleaner test -v
go -C services/common test -v ./...

task review:install
reviewer/.venv/bin/python  -m unittest reviewer.test_issue_agent
reviewer/.venv/bin/python  -m unittest reviewer.test_check_limits
reviewer/.venv/bin/python  -m unittest reviewer.test_reviewer

## This is the end of tests if the script was run from agentic-workflow suite
## or when script was run in standalone mode.
if [ -z "$METRICS_PORT_AIWEIGHTER" ]; then
  echo "METRICS_PORT_AIWEIGHTER is not set. Further tests are not possible."
  echo "Skipping further tests. Treating as successful."
  ## This is questionable, I know.
  ## For now I will live with this.
  exit 0
fi

function poll_for_devenv_up_status {
  local max_attempts=30
  local attempt=1
  while [ $attempt -le $max_attempts ]; do
      if devenv processes list >/dev/null 2>&1; then
          echo "Devenv is up and running."
          return 0
      else
          echo "Waiting for devenv to be up... attempt $attempt/$max_attempts"
          sleep 5
          attempt=$((attempt + 1))
      fi
  done
  SECONDS_WAITED=$((max_attempts * 5))
  echo "Waited $SECONDS_WAITED seconds for devenv to start."
  echo "Devenv did not start within the expected time."
  return 1
}

echo "Unit tests of CV Platform services are done."
echo "----------------------------------------"
echo "Proceeding with Smoke Tests for services..."

if ! devenv version >/dev/null 2>&1; then
    echo "Devenv is not installed or not working properly in this environment."
    echo "Cannot continue without a working devenv installation."
    exit 1
fi
# First, check if devenv is working in the background.
# `devenv processes list` should exit 1 if devenv is not running
# If that's the case, we should just... start it in background, why not..
DEVENV_RUNNING=false
if ! devenv processes list >/dev/null 2>&1; then
    DEVENV_RUNNING=true
    echo "Devenv is not running, starting it in the background..."
    devenv up -d 2>/dev/null &
    # Give it some time to start
    echo "Devenv is starting right now... waiting for it to be ready."
    poll_for_devenv_up_status
fi

FAILED_METRICS_COUNT=0
function check_metric {
  local port=$1
  if curl -s "http://localhost:$port/metrics" >/dev/null 2>&1; then
    echo "Successfully fetched metrics from port $port"
  else
    echo "Failed to fetch metrics from port $port"
    FAILED_METRICS_COUNT=$((FAILED_METRICS_COUNT + 1))
  fi
}

check_metric $METRICS_PORT_AIWEIGHTER
check_metric $METRICS_PORT_EXTRACTOR
check_metric $METRICS_PORT_PIISTRIPPER
check_metric $METRICS_PORT_SKILLMATCHER
check_metric $METRICS_PORT_OFFERMATCHER
check_metric $METRICS_PORT_ROLEINSIGHTS
check_metric $METRICS_PORT_DATACLEANER

if [ "$DEVENV_RUNNING" = true ]; then
    echo "Devenv was started by this script, stopping it now..."
    devenv down >/dev/null 2>&1
fi

if [ "$FAILED_METRICS_COUNT" -ne 0 ]; then
    echo "Some metrics checks failed: $FAILED_METRICS_COUNT"
    exit 1
fi
