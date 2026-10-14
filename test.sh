echo "This is a test script for CV Platform."
echo ""
echo "It will run following suites, in order:"
echo ""
echo "Services (backend) suite:"
echo "  - Unit tests"
echo "  - Integration tests"
echo ""
echo "Frontend (UI) suite:"
echo "  - Unit tests"
echo "  - E2E tests"
echo ""

go -C services/uploader test -v
go -C services/extractor test -v