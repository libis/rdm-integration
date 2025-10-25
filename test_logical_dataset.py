#!/usr/bin/env python3
"""Quick test to verify LogicalDataSet has proper metadata."""

import sys
import json
from pathlib import Path

# Add image directory to path
sys.path.insert(0, str(Path(__file__).parent / "image"))

import cdi_generator

def test_logical_dataset_metadata():
    """Test that LogicalDataSet gets proper URI, identifier, label, and description."""
    
    # Load test metadata
    metadata_path = Path(__file__).parent / "image" / "testdata" / "dataset_metadata.json"
    metadata = json.loads(metadata_path.read_text())
    
    # Extract title and description
    title = cdi_generator.extract_dataset_title(metadata)
    description = cdi_generator.extract_dataset_description(metadata)
    
    print(f"✓ Extracted title: {title}")
    print(f"✓ Extracted description: {description}")
    
    # Create a minimal manifest
    csv_path = Path(__file__).parent / "image" / "testdata" / "sample.csv"
    if not csv_path.exists():
        print("✗ Test CSV not found, creating minimal one...")
        csv_path.write_text("name,age\nAlice,30\nBob,25\n")
    
    manifest = {
        "dataset_pid": "doi:10.123/test",
        "dataset_uri_base": "https://example.org/dataset",
        "dataset_metadata_path": str(metadata_path),
        "files": [
            {
                "csv_path": str(csv_path),
                "file_name": "sample.csv",
                "allow_xconvert": False,
            }
        ]
    }
    
    output_path = Path("/tmp/test_cdi_output.ttl")
    warnings, total_rows, files_processed = cdi_generator.generate_manifest_cdi(
        manifest=manifest,
        output_path=output_path,
        summary_json=None,
        skip_md5_default=True,
        quiet=True,
    )
    
    print(f"✓ Generated CDI output: {files_processed} files, {total_rows} rows")
    if warnings:
        print(f"  Warnings: {warnings}")
    
    # Check the output
    output = output_path.read_text()
    
    checks = [
        ("LogicalDataSet type", "a cdi:LogicalDataSet"),
        ("LogicalDataSet URI (not blank node)", "#logical/"),
        ("LogicalDataSet identifier", "dcterms:identifier"),
        ("LogicalDataSet label", "skos:prefLabel"),
        ("LogicalDataSet description", "dcterms:description"),
        ("Dataset description in output", title if title else "Test Dataset"),
    ]
    
    all_passed = True
    for check_name, pattern in checks:
        if pattern in output:
            print(f"✓ {check_name}: found '{pattern}'")
        else:
            print(f"✗ {check_name}: missing '{pattern}'")
            all_passed = False
    
    # Print a snippet of the LogicalDataSet section
    print("\n--- LogicalDataSet snippet ---")
    lines = output.split('\n')
    in_logical = False
    snippet_lines = []
    for line in lines:
        if '#logical/' in line:
            in_logical = True
        if in_logical:
            snippet_lines.append(line)
            if line.strip() == '' or (line.strip() and not line.startswith(' ')):
                if len(snippet_lines) > 1:
                    break
    
    print('\n'.join(snippet_lines[:15]))
    print("--- End snippet ---\n")
    
    if all_passed:
        print("✅ All checks passed!")
        return 0
    else:
        print("❌ Some checks failed")
        return 1

if __name__ == "__main__":
    sys.exit(test_logical_dataset_metadata())
