#!/bin/bash
# Test runner for RDM Integration DDI-CDI feature
# This script runs both Python and Go tests with proper setup and cleanup

set -e  # Exit on error

# Get the directory where this script is located
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$PROJECT_ROOT"

# Check if asdf is being used and set Python version if needed
if [ -f "$HOME/.asdf/asdf.sh" ] && command -v asdf &> /dev/null; then
    # If .tool-versions doesn't exist, create one with available Python
    if [ ! -f .tool-versions ] && asdf list python &> /dev/null; then
        PYTHON_VERSION=$(asdf list python | grep -v '/' | tail -1 | tr -d ' ')
        if [ -n "$PYTHON_VERSION" ]; then
            echo "python $PYTHON_VERSION" > .tool-versions
        fi
    fi
fi
echo "================================"
echo "RDM Integration Test Suite"
echo "================================"
echo ""

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
PYTHON_CMD=""
if command -v python3.13 &> /dev/null; then
    PYTHON_CMD="python3.13"
elif asdf which python3 &> /dev/null; then
    # Use asdf Python if available
    asdf local python 3.13.7 2>/dev/null || true
    PYTHON_CMD="python3"
elif command -v python3 &> /dev/null; then
    PYTHON_CMD="python3"
elif command -v python &> /dev/null; then
    PYTHON_CMD="python"
else
    echo -e "${RED}Error: No Python installation found${NC}"
    exit 1
fi

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
echo -e "${YELLOW}Running Python tests for cdi_generator.py...${NC}"
if python3 test_csv_to_cdi.py; then
    echo -e "${GREEN}✓${NC} Python tests passed"
else
    echo -e "${RED}✗${NC} Python tests failed"
    FAILURES=$((FAILURES + 1))
fi
echo ""

# Test cdi_generator.py with sample data
echo -e "${YELLOW}Testing cdi_generator.py with sample data...${NC}"
if python3 cdi_generator.py \
    --csv testdata/sample.csv \
    --dataset-pid "doi:10.123/TEST" \
    --dataset-uri-base "https://example.org/dataset" \
    --output testdata/output_sample.ttl \
    --skip-md5 \
    --quiet; then
    echo -e "${GREEN}✓${NC} cdi_generator.py execution successful"
    
    # Verify output file was created
    if [ -f "testdata/output_sample.ttl" ]; then
        echo -e "${GREEN}✓${NC} Output file created"
        
        # Check if output contains expected RDF elements
        if grep -q "@prefix cdi:" testdata/output_sample.ttl && \
           grep -q "cdi:DataSet" testdata/output_sample.ttl && \
           grep -q "cdi:Variable" testdata/output_sample.ttl; then
            echo -e "${GREEN}✓${NC} Output contains valid CDI RDF"
        else
            echo -e "${RED}✗${NC} Output missing expected RDF elements"
            FAILURES=$((FAILURES + 1))
        fi
    else
        echo -e "${RED}✗${NC} Output file not created"
        FAILURES=$((FAILURES + 1))
    fi
else
    echo -e "${RED}✗${NC} cdi_generator.py execution failed"
    FAILURES=$((FAILURES + 1))
fi
echo ""

# Test with DDI metadata
echo -e "${YELLOW}Testing cdi_generator.py with DDI metadata...${NC}"
if python3 cdi_generator.py \
    --csv testdata/sample.csv \
    --dataset-pid "doi:10.123/TEST-DDI" \
    --dataset-uri-base "https://example.org/dataset" \
    --ddi-file testdata/sample_ddi.xml \
    --output testdata/output_with_ddi.ttl \
    --skip-md5 \
    --quiet; then
    echo -e "${GREEN}✓${NC} cdi_generator.py with DDI successful"
    
    # Check if DDI metadata is included
    if grep -q "DDI categories:" testdata/output_with_ddi.ttl || \
       grep -q "DDI stats:" testdata/output_with_ddi.ttl; then
        echo -e "${GREEN}✓${NC} DDI metadata included in output"
    else
        echo -e "${YELLOW}⚠${NC} No DDI metadata found in output (may be expected)"
    fi
else
    echo -e "${RED}✗${NC} cdi_generator.py with DDI failed"
    FAILURES=$((FAILURES + 1))
fi
echo ""

# Test with dataset metadata
echo -e "${YELLOW}Testing cdi_generator.py with dataset metadata...${NC}"
if python3 cdi_generator.py \
    --csv testdata/sample.csv \
    --dataset-pid "doi:10.123/TEST-META" \
    --dataset-uri-base "https://example.org/dataset" \
    --dataset-metadata-file testdata/dataset_metadata.json \
    --output testdata/output_with_metadata.ttl \
    --skip-md5 \
    --quiet; then
    echo -e "${GREEN}✓${NC} cdi_generator.py with metadata successful"
    
    # Check if dataset title is included
    if grep -q "Test Dataset for DDI-CDI" testdata/output_with_metadata.ttl; then
        echo -e "${GREEN}✓${NC} Dataset title included from metadata"
    else
        echo -e "${YELLOW}⚠${NC} Dataset title not found (may use default)"
    fi
else
    echo -e "${RED}✗${NC} cdi_generator.py with metadata failed"
    FAILURES=$((FAILURES + 1))
fi
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
