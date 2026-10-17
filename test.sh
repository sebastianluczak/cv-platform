#!/bin/bash
set -eu

echo "This is a test script for CV Platform."
echo ""
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

# Crazy idea - start e2e tests here...
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
    source .env.dist # just to be sure
    echo "Devenv is not running, starting it in the background..."
    devenv up -d >/dev/null 2>&1 &
    # Give it some time to start
    echo "Devenv is starting right now... waiting 20 seconds for it to be ready."
    sleep 20
fi

function check_metric {
    local port=$1
    # do not print output
    CURL_CHECK=$(curl -s "http://localhost:$port/metrics" >/dev/null 2>&1)
    if [ $? -ne 0 ]; then
        echo "Failed to fetch metrics from port $port"
    else
        echo "Successfully fetched metrics from port $port"
    fi
}

# Now, run the e2e tests...
# First simple test, is to curl the metrics endpoints.
# TODO - change that to healthz if enabled for all microservices
check_metric $METRICS_PORT_AIWEIGHTER
check_metric $METRICS_PORT_EXTRACTOR
check_metric $METRICS_PORT_PIISTRIPPER
check_metric $METRICS_PORT_SKILLMATCHER
check_metric $METRICS_PORT_OFFERMATCHER
check_metric $METRICS_PORT_ROLEINSIGHTS
check_metric $METRICS_PORT_DATACLEANER

# omg, this will not work at all...
if [ "$DEVENV_RUNNING" = true ]; then
    echo "Devenv was started by this script, stopping it now..."
    devenv down >/dev/null 2>&1
fi

# I just did smoke tests, not e2e tests... I'm ok with this for now.