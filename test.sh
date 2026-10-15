set -eu

echo "This is a test script for CV Platform."
echo ""
go -C services/uploader test -v
go -C services/extractor test -v
go -C services/piistripper test -v
go -C services/skillmatcher test -v
go -C services/offermatcher test -v
go -C services/common test -v ./...

task review:install
reviewer/.venv/bin/python  -m unittest reviewer.test_issue_agent
reviewer/.venv/bin/python  -m unittest reviewer.test_check_limits
reviewer/.venv/bin/python  -m unittest reviewer.test_reviewer
