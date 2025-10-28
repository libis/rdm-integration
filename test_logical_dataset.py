#!/usr/bin/env python3
"""Quick test to verify LogicalDataSet has proper metadata."""

import sys
import json
from pathlib import Path

# Add image directory to path
sys.path.insert(0, str(Path(__file__).parent / "image"))

import cdi_generator  # type: ignore
from rdflib import BNode, Graph, Namespace, RDF, URIRef

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
    
    graph = Graph()
    graph.parse(data=output, format="turtle")

    cdi_ns = Namespace("http://www.ddialliance.org/Specification/DDI-CDI/1.0/RDF/")
    dcterms_ns = Namespace("http://purl.org/dc/terms/")
    skos_ns = Namespace("http://www.w3.org/2004/02/skos/core#")
    dataset_uri = URIRef("https://example.org/dataset/" + manifest["dataset_pid"])

    logical_links = list(graph.triples((dataset_uri, cdi_ns.hasLogicalDataSet, None)))
    physical_links = list(graph.triples((dataset_uri, cdi_ns.hasPhysicalDataSet, None)))

    if not logical_links:
        print("❌ No LogicalDataSet links found")
        return 1

    if not physical_links:
        print("❌ No PhysicalDataSet links found")
        return 1

    print(f"✓ Found {len(logical_links)} LogicalDataSet link(s)")
    print(f"✓ Found {len(physical_links)} PhysicalDataSet link(s)")

    all_passed = True
    for idx, (_, _, logical_node) in enumerate(logical_links, start=1):
        if not isinstance(logical_node, BNode):
            print(f"✗ LogicalDataSet {idx} is not a blank node: {logical_node}")
            all_passed = False
            continue

        has_type = (logical_node, RDF.type, cdi_ns.LogicalDataSet) in graph
        identifier = graph.value(logical_node, dcterms_ns.identifier)
        label = graph.value(logical_node, skos_ns.prefLabel)
        description = graph.value(logical_node, dcterms_ns.description)

        if has_type and identifier and label and description:
            print(
                f"✓ LogicalDataSet {idx}: typed, has identifier '{identifier}', label '{label}', description present"
            )
        else:
            print(f"✗ LogicalDataSet {idx} is missing required metadata")
            print(f"  type: {has_type}, identifier: {identifier}, label: {label}, description: {description}")
            all_passed = False

    for idx, (_, _, physical_node) in enumerate(physical_links, start=1):
        if not isinstance(physical_node, BNode):
            print(f"✗ PhysicalDataSet {idx} is not a blank node: {physical_node}")
            all_passed = False
        else:
            print(f"✓ PhysicalDataSet {idx} is a blank node")

    if all_passed:
        print("✅ All checks passed!")
        return 0

    print("❌ Some checks failed")
    return 1

if __name__ == "__main__":
    sys.exit(test_logical_dataset_metadata())
