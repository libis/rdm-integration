#!/bin/bash
# Test runner for RDM Integration DDI-CDI feature
# This script runs both Python and Go tests with proper setup and cleanup

set -e  # Exit on error

# Get the directory where this script is located
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$PROJECT_ROOT"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Track failures
FAILURES=0

# Clean up function
cleanup() {
    echo ""
    echo "Cleaning up test artifacts..."
    
    # Clean Go test cache
    go clean -testcache 2>/dev/null || true
    
    # Remove Python cache
    find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
    find . -type f -name "*.pyc" -delete 2>/dev/null || true
    
    # Deactivate virtual environment if active
    if [ -n "$VIRTUAL_ENV" ]; then
        deactivate 2>/dev/null || true
    fi
    
    # Remove virtual environment
    if [ -d "venv" ]; then
        rm -rf venv
    fi
    
    # Remove temporary test files
    rm -rf /tmp/test_cdi_* 2>/dev/null || true
    rm -rf testdata/output_* 2>/dev/null || true
    
    echo "Cleanup complete."
}

# Set up xconvert path for tests
if [ -f "../xconvert" ]; then
    export XCONVERT_PATH="$(cd .. && pwd)/xconvert"
    echo "xconvert binary found at: $XCONVERT_PATH"
    if [ ! -x "$XCONVERT_PATH" ]; then
        chmod +x "$XCONVERT_PATH"
        echo "Made xconvert executable"
    fi
else
    echo "Warning: xconvert not found at ../xconvert - xconvert tests will be skipped"
fi

# Register cleanup on exit
trap cleanup EXIT

# ================================
# Python Tests
# ================================
echo -e "${YELLOW}Setting up Python environment...${NC}"

# Find Python executable
PYTHON_CMD="python"

echo "Using Python: $PYTHON_CMD ($($PYTHON_CMD --version 2>&1))"

# Create virtual environment
$PYTHON_CMD -m venv venv
source venv/bin/activate

# Install dependencies
echo "Installing Python dependencies..."
pip install --quiet --upgrade pip
pip install --quiet -r requirements.txt

echo -e "${GREEN}✓${NC} Python environment ready"
echo ""

# Run Python tests
echo -e "${YELLOW}Running Python tests for cdi_generator_jsonld.py...${NC}"
if python3 test_cdi_generator_jsonld.py; then
    echo -e "${GREEN}✓${NC} Python tests passed"
else
    echo -e "${RED}✗${NC} Python tests failed"
    FAILURES=$((FAILURES + 1))
fi
echo ""

# Test cdi_generator_jsonld.py with sample data (manifest mode)
echo -e "${YELLOW}Testing cdi_generator_jsonld.py with sample data...${NC}"

# Create manifest for test
SAMPLE_MANIFEST=$(mktemp)
cat > "$SAMPLE_MANIFEST" << 'EOF'
{
    "dataset_pid": "doi:10.123/TEST",
    "dataset_uri_base": "https://example.org/dataset",
    "files": [
        {"csv_path": "testdata/sample.csv"}
    ]
}
EOF

if python3 cdi_generator_jsonld.py \
    --manifest "$SAMPLE_MANIFEST" \
    --output testdata/output_sample.jsonld \
    --skip-md5 \
    --quiet; then
    echo -e "${GREEN}✓${NC} cdi_generator_jsonld.py execution successful"
    
    # Verify output file was created
    if [ -f "testdata/output_sample.jsonld" ]; then
        echo -e "${GREEN}✓${NC} Output file created"
        
        # Check if output contains expected JSON-LD elements
        if grep -q "@context" testdata/output_sample.jsonld && \
           grep -q "@graph" testdata/output_sample.jsonld && \
           grep -q "WideDataSet" testdata/output_sample.jsonld; then
            echo -e "${GREEN}✓${NC} Output contains valid DDI-CDI JSON-LD"
        else
            echo -e "${RED}✗${NC} Output missing expected JSON-LD elements"
            FAILURES=$((FAILURES + 1))
        fi
    else
        echo -e "${RED}✗${NC} Output file not created"
        FAILURES=$((FAILURES + 1))
    fi
else
    echo -e "${RED}✗${NC} cdi_generator_jsonld.py execution failed"
    FAILURES=$((FAILURES + 1))
fi
rm -f "$SAMPLE_MANIFEST"
echo ""

# Test with DDI metadata (manifest mode)
echo -e "${YELLOW}Testing cdi_generator_jsonld.py with DDI metadata...${NC}"

DDI_MANIFEST=$(mktemp)
cat > "$DDI_MANIFEST" << 'EOF'
{
    "dataset_pid": "doi:10.123/TEST-DDI",
    "dataset_uri_base": "https://example.org/dataset",
    "files": [
        {"csv_path": "testdata/sample.csv", "ddi_path": "testdata/sample_ddi.xml"}
    ]
}
EOF

if python3 cdi_generator_jsonld.py \
    --manifest "$DDI_MANIFEST" \
    --output testdata/output_with_ddi.jsonld \
    --skip-md5 \
    --quiet; then
    echo -e "${GREEN}✓${NC} cdi_generator_jsonld.py with DDI successful"
    
    # Check if output is valid JSON-LD
    if python3 -c "import json; json.load(open('testdata/output_with_ddi.jsonld'))"; then
        echo -e "${GREEN}✓${NC} Output is valid JSON"
    else
        echo -e "${RED}✗${NC} Output is not valid JSON"
        FAILURES=$((FAILURES + 1))
    fi
else
    echo -e "${RED}✗${NC} cdi_generator_jsonld.py with DDI failed"
    FAILURES=$((FAILURES + 1))
fi
rm -f "$DDI_MANIFEST"
echo ""

# Test with dataset metadata (manifest mode)
echo -e "${YELLOW}Testing cdi_generator_jsonld.py with dataset metadata...${NC}"

META_MANIFEST=$(mktemp)
cat > "$META_MANIFEST" << 'EOF'
{
    "dataset_pid": "doi:10.123/TEST-META",
    "dataset_uri_base": "https://example.org/dataset",
    "dataset_metadata_file": "testdata/dataset_metadata.json",
    "files": [
        {"csv_path": "testdata/sample.csv"}
    ]
}
EOF

if python3 cdi_generator_jsonld.py \
    --manifest "$META_MANIFEST" \
    --output testdata/output_with_metadata.jsonld \
    --skip-md5 \
    --quiet; then
    echo -e "${GREEN}✓${NC} cdi_generator_jsonld.py with metadata successful"
    
    # Check if dataset title is included
    if grep -q "Test Dataset for DDI-CDI" testdata/output_with_metadata.jsonld; then
        echo -e "${GREEN}✓${NC} Dataset title included from metadata"
    else
        echo -e "${YELLOW}⚠${NC} Dataset title not found (may use default)"
    fi
else
    echo -e "${RED}✗${NC} cdi_generator_jsonld.py with metadata failed"
    FAILURES=$((FAILURES + 1))
fi
rm -f "$META_MANIFEST"
echo ""

# ================================
# Go Tests
# ================================
echo -e "${YELLOW}Running Go tests...${NC}"

cd app

# Run all Go tests with coverage
if go test -v -race -cover ./... 2>&1 | tee ../test_output.log; then
    echo -e "${GREEN}✓${NC} Go tests passed"
else
    echo -e "${RED}✗${NC} Go tests failed"
    FAILURES=$((FAILURES + 1))
fi
echo ""

# Generate coverage report
echo -e "${YELLOW}Generating Go coverage report...${NC}"
if go test -coverprofile=../coverage.out ./... > /dev/null 2>&1; then
    go tool cover -html=../coverage.out -o ../coverage.html
    
    # Calculate coverage percentage
    COVERAGE=$(go tool cover -func=../coverage.out | grep total | awk '{print $3}')
    echo -e "${GREEN}✓${NC} Coverage report generated: ${COVERAGE}"
    echo "   HTML report: coverage.html"
else
    echo -e "${YELLOW}⚠${NC} Coverage report generation failed"
fi
echo ""

cd ..

# ================================
# Summary
# ================================
echo "================================"
echo "Test Summary"
echo "================================"

if [ $FAILURES -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}$FAILURES test suite(s) failed${NC}"
    exit 1
fi
