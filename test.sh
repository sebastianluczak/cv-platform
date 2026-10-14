set -eu

echo "This is a test script for CV Platform."
echo ""
echo "It will run the following backend test suites:"
echo ""
go -C services/uploader test -v
go -C services/extractor test -v
go -C services/piistripper test -v
go -C services/skillmatcher test -v
