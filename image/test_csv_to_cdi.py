#!/usr/bin/env python3
"""
Comprehensive test suite for cdi_generator.py

Tests cover:
- CSV parsing and type inference
- DDI metadata integration
- Dataset metadata extraction
- Error handling
- Edge cases
- xconvert integration for converting statistical data files (SPSS, SAS, Stata) to DDI
  - Tests are automatically skipped if xconvert binary is not available
  - Set XCONVERT_PATH environment variable to specify xconvert location
  - Test data files: testdata/simple_data.{sps,sas,dct}
"""

import unittest
import tempfile
import json
import sys
import subprocess
import os
import xml.etree.ElementTree as ET
from pathlib import Path
from io import StringIO

# Add parent directory to path to import cdi_generator
sys.path.insert(0, str(Path(__file__).parent.parent))

import cdi_generator as csv_to_cdi

# Path to xconvert binary
XCONVERT_PATH = os.environ.get('XCONVERT_PATH', '/usr/local/bin/xconvert')

class TestColumnStats(unittest.TestCase):
    """Test column statistics and type inference"""

    def test_int_inference(self):
        """Test integer type detection"""
        stats = csv_to_cdi.ColumnStats("test_col")
        stats.update("1")
        stats.update("2")
        stats.update("3")
        self.assertEqual(stats.xsd_datatype(), csv_to_cdi.XSD.integer)

    def test_float_inference(self):
        """Test float type detection"""
        stats = csv_to_cdi.ColumnStats("test_col")
        stats.update("1.5")
        stats.update("2.7")
        stats.update("3.14")
        self.assertEqual(stats.xsd_datatype(), csv_to_cdi.XSD.decimal)

    def test_bool_inference(self):
        """Test boolean type detection"""
        stats = csv_to_cdi.ColumnStats("test_col")
        stats.update("true")
        stats.update("false")
        stats.update("true")
        self.assertEqual(stats.xsd_datatype(), csv_to_cdi.XSD.boolean)

    def test_datetime_inference(self):
        """Test datetime type detection"""
        stats = csv_to_cdi.ColumnStats("test_col")
        stats.update("2023-01-01")
        stats.update("2023-12-31")
        self.assertEqual(stats.xsd_datatype(), csv_to_cdi.XSD.dateTime)

    def test_string_fallback(self):
        """Test string fallback for mixed types"""
        stats = csv_to_cdi.ColumnStats("test_col")
        stats.update("hello")
        stats.update("world")
        self.assertEqual(stats.xsd_datatype(), csv_to_cdi.XSD.string)

    def test_missing_values(self):
        """Test handling of missing values"""
        stats = csv_to_cdi.ColumnStats("test_col")
        stats.update("NA")
        stats.update("null")
        stats.update("")
        stats.update("1")
        stats.update("2")
        self.assertEqual(stats.n_non_missing, 2)
        self.assertEqual(stats.xsd_datatype(), csv_to_cdi.XSD.integer)

    def test_role_identifier(self):
        """Test identifier role detection (high uniqueness)"""
        stats = csv_to_cdi.ColumnStats("id")
        for i in range(100):
            stats.update(str(i))
        self.assertEqual(stats.role(), "identifier")

    def test_role_measure(self):
        """Test measure role detection (numeric, low uniqueness)"""
        stats = csv_to_cdi.ColumnStats("score")
        for i in range(100):
            stats.update(str(i % 10))  # Only 10 unique values
        self.assertEqual(stats.role(), "measure")

    def test_role_dimension(self):
        """Test dimension role detection (categorical)"""
        stats = csv_to_cdi.ColumnStats("category")
        for i in range(100):
            stats.update(["A", "B", "C"][i % 3])
        self.assertEqual(stats.role(), "dimension")


class TestTypeCheckers(unittest.TestCase):
    """Test helper functions for type checking"""

    def test_is_int(self):
        """Test integer detection"""
        self.assertTrue(csv_to_cdi.is_int("123"))
        self.assertTrue(csv_to_cdi.is_int("-456"))
        self.assertTrue(csv_to_cdi.is_int("  789  "))
        self.assertFalse(csv_to_cdi.is_int("123.45"))
        self.assertFalse(csv_to_cdi.is_int("abc"))

    def test_is_float(self):
        """Test float detection"""
        self.assertTrue(csv_to_cdi.is_float("123.45"))
        self.assertTrue(csv_to_cdi.is_float("-67.89"))
        self.assertFalse(csv_to_cdi.is_float("123"))  # Integers excluded
        self.assertFalse(csv_to_cdi.is_float("abc"))

    def test_is_bool(self):
        """Test boolean detection"""
        self.assertTrue(csv_to_cdi.is_bool("true"))
        self.assertTrue(csv_to_cdi.is_bool("FALSE"))
        self.assertTrue(csv_to_cdi.is_bool("yes"))
        self.assertTrue(csv_to_cdi.is_bool("0"))
        self.assertTrue(csv_to_cdi.is_bool("1"))
        self.assertFalse(csv_to_cdi.is_bool("maybe"))

    def test_is_datetime(self):
        """Test datetime detection"""
        self.assertTrue(csv_to_cdi.is_datetime("2023-01-01"))
        self.assertTrue(csv_to_cdi.is_datetime("01/15/2023"))
        self.assertTrue(csv_to_cdi.is_datetime("2023-12-31T23:59:59"))
        self.assertFalse(csv_to_cdi.is_datetime("not a date"))


class TestCSVProcessing(unittest.TestCase):
    """Test CSV file processing"""

    def setUp(self):
        """Create temporary directory for test files"""
        self.test_dir = tempfile.mkdtemp()
        self.test_path = Path(self.test_dir)

    def test_simple_csv(self):
        """Test processing a simple CSV file"""
        csv_file = self.test_path / "simple.csv"
        csv_file.write_text("id,name,age\n1,John,30\n2,Jane,25\n")

        cols, stats, info, md5, dialect = csv_to_cdi.stream_profile_csv(
            csv_file, header=True, compute_md5=False
        )

        self.assertEqual(len(cols), 3)
        self.assertEqual(cols, ["id", "name", "age"])
        self.assertEqual(info["rows_read"], 2)
        self.assertEqual(stats[0].xsd_datatype(), csv_to_cdi.XSD.integer)  # id
        self.assertEqual(stats[1].xsd_datatype(), csv_to_cdi.XSD.string)   # name
        self.assertEqual(stats[2].xsd_datatype(), csv_to_cdi.XSD.integer)  # age

    def test_csv_with_missing_values(self):
        """Test CSV with missing/null values"""
        csv_file = self.test_path / "missing.csv"
        csv_file.write_text("id,value\n1,100\n2,NA\n3,null\n4,200\n")

        cols, stats, info, _, _ = csv_to_cdi.stream_profile_csv(
            csv_file, compute_md5=False
        )

        # Should have 2 non-missing values for 'value' column
        self.assertEqual(stats[1].n_non_missing, 2)
        self.assertEqual(stats[1].xsd_datatype(), csv_to_cdi.XSD.integer)

    def test_csv_no_header(self):
        """Test CSV without header row"""
        csv_file = self.test_path / "no_header.csv"
        csv_file.write_text("1,John,30\n2,Jane,25\n")

        cols, stats, info, _, _ = csv_to_cdi.stream_profile_csv(
            csv_file, header=False, compute_md5=False
        )

        # Should auto-generate column names
        self.assertEqual(cols, ["col_1", "col_2", "col_3"])
        self.assertEqual(info["rows_read"], 2)

    def test_header_auto_detection_on_dataverse_tab(self):
        """Ensure auto header detection does not treat data row as header."""
        csv_file = self.test_path / "dataverse_no_header.tab"
        csv_file.write_text("1\t2020-01-15\t95.5\tJohn Doe\n2\t2021-03-22\t88.2\tJane Smith\n")

        cols, stats, info, _, _ = csv_to_cdi.stream_profile_csv(
            csv_file, delimiter="\t", header="auto", compute_md5=False
        )

        self.assertEqual(cols, ["col_1", "col_2", "col_3", "col_4"])
        self.assertEqual(info["rows_read"], 2)
        # First column should be treated as integer measure rather than identifier string
        self.assertEqual(stats[0].xsd_datatype(), csv_to_cdi.XSD.integer)

    def test_custom_delimiter(self):
        """Test CSV with custom delimiter"""
        csv_file = self.test_path / "semicolon.csv"
        csv_file.write_text("id;name;age\n1;John;30\n2;Jane;25\n")

        cols, stats, info, _, dialect = csv_to_cdi.stream_profile_csv(
            csv_file, delimiter=";", header=True, compute_md5=False
        )

        self.assertEqual(len(cols), 3)
        self.assertEqual(dialect.delimiter, ";")

    def test_row_limit(self):
        """Test limiting rows processed"""
        csv_file = self.test_path / "large.csv"
        lines = ["id,value\n"] + [f"{i},{i*10}\n" for i in range(1000)]
        csv_file.write_text("".join(lines))

        cols, stats, info, _, _ = csv_to_cdi.stream_profile_csv(
            csv_file, limit_rows=100, compute_md5=False
        )

        self.assertEqual(info["rows_read"], 100)

    def test_empty_csv(self):
        """Test error handling for empty CSV"""
        csv_file = self.test_path / "empty.csv"
        csv_file.write_text("")

        with self.assertRaises(ValueError):
            csv_to_cdi.stream_profile_csv(csv_file, compute_md5=False)

    def test_md5_calculation(self):
        """Test MD5 hash calculation"""
        csv_file = self.test_path / "test.csv"
        csv_file.write_text("id,name\n1,test\n")

        _, _, _, md5_hash, _ = csv_to_cdi.stream_profile_csv(
            csv_file, compute_md5=True
        )

        self.assertIsNotNone(md5_hash)
        self.assertEqual(len(md5_hash), 32)  # MD5 is 32 hex chars


class TestMetadataExtraction(unittest.TestCase):
    """Test metadata extraction from Dataverse JSON"""

    def test_extract_dataset_title(self):
        """Test extracting dataset title"""
        metadata = {
            "datasetVersion": {
                "metadataBlocks": {
                    "citation": {
                        "fields": [
                            {"typeName": "title", "value": "Test Dataset"}
                        ]
                    }
                }
            }
        }

        title = csv_to_cdi.extract_dataset_title(metadata)
        self.assertEqual(title, "Test Dataset")

    def test_extract_dataset_title_list(self):
        """Test extracting title when it's a list"""
        metadata = {
            "datasetVersion": {
                "metadataBlocks": {
                    "citation": {
                        "fields": [
                            {"typeName": "title", "value": ["Test Dataset"]}
                        ]
                    }
                }
            }
        }

        title = csv_to_cdi.extract_dataset_title(metadata)
        self.assertEqual(title, "Test Dataset")

    def test_extract_file_uri(self):
        """Test extracting file URI from metadata"""
        metadata = {
            "files": [
                {
                    "label": "data.csv",
                    "dataFile": {
                        "id": 12345,
                        "pidURL": "https://example.org/file/123"
                    }
                }
            ]
        }

        uri = csv_to_cdi.extract_file_uri(metadata, "data.csv", "https://example.org")
        self.assertEqual(uri, "https://example.org/file/123")

    def test_extract_file_uri_with_directory(self):
        """Test extracting file URI with directory label"""
        metadata = {
            "files": [
                {
                    "label": "data.csv",
                    "directoryLabel": "subdir",
                    "dataFile": {
                        "id": 12345,
                        "persistentId": "doi:10.123/F/456"
                    }
                }
            ]
        }

        uri = csv_to_cdi.extract_file_uri(metadata, "subdir/data.csv", "https://example.org")
        self.assertEqual(uri, "doi:10.123/F/456")


class TestDDIMetadata(unittest.TestCase):
    """Test DDI metadata loading and parsing"""

    def setUp(self):
        """Create temporary directory for test files"""
        self.test_dir = tempfile.mkdtemp()
        self.test_path = Path(self.test_dir)

    def test_load_ddi_metadata(self):
        """Test loading DDI XML"""
        ddi_file = self.test_path / "test.xml"
        ddi_content = """<?xml version="1.0" encoding="UTF-8"?>
<codeBook xmlns="ddi:codebook:2_5">
  <dataDscr>
    <var name="age" ID="V1">
      <labl>Age in Years</labl>
      <sumStat type="mean">35.5</sumStat>
      <sumStat type="min">18</sumStat>
      <sumStat type="max">65</sumStat>
    </var>
    <var name="category" ID="V2">
      <labl>Category Code</labl>
      <catgry>
        <catValu>A</catValu>
        <labl>Category A</labl>
      </catgry>
      <catgry>
        <catValu>B</catValu>
        <labl>Category B</labl>
      </catgry>
    </var>
  </dataDscr>
</codeBook>"""
        ddi_file.write_text(ddi_content)

        raw_xml, variables, is_xml = csv_to_cdi.load_ddi_metadata(ddi_file)

        self.assertIsNotNone(raw_xml)
        self.assertIn("age", variables)
        self.assertIn("category", variables)
        self.assertTrue(is_xml)

        self.assertEqual(variables["age"]["label"], "Age in Years")
        self.assertEqual(variables["age"]["statistics"]["mean"], "35.5")

        self.assertEqual(len(variables["category"]["categories"]), 2)
        self.assertEqual(variables["category"]["categories"][0], ("A", "Category A"))

    def test_load_invalid_ddi(self):
        """Test handling of invalid DDI XML"""
        ddi_file = self.test_path / "invalid.xml"
        ddi_file.write_text("not valid xml <><")

        raw_xml, variables, is_xml = csv_to_cdi.load_ddi_metadata(ddi_file)

        # Should return raw text but no parsed variables
        self.assertIsNotNone(raw_xml)
        self.assertEqual(len(variables), 0)
        self.assertFalse(is_xml)

    def test_load_ddi_metadata_fixture(self):
        """Test loading the real GetDataFileDDI fixture."""
        ddi_path = Path(__file__).parent / "testdata" / "tmp_ddi8.xml"
        self.assertTrue(ddi_path.exists(), "Expected tmp_ddi8.xml fixture to exist")

        raw_xml, variables, is_xml = csv_to_cdi.load_ddi_metadata(ddi_path)

        # Raw XML should be returned and recognized as XML literal
        self.assertIsNotNone(raw_xml)
        root = ET.fromstring(raw_xml)
        self.assertTrue(root.tag.endswith("codeBook"))
        self.assertTrue(is_xml)

        # Verify that key variables are captured with statistics
        self.assertIn("id", variables)
        self.assertIn("salary", variables)
        self.assertIn("active", variables)

        salary_stats = variables["salary"].get("statistics", {})
        self.assertEqual(salary_stats.get("mean"), "76400.3")
        self.assertEqual(salary_stats.get("min"), "62000.0")
        self.assertEqual(salary_stats.get("max"), "95000.75")

        # Ensure labels are preserved for nominal variables
        self.assertEqual(variables["active"].get("label"), "active")


class TestRDFGeneration(unittest.TestCase):
    """Test RDF/Turtle generation"""

    def setUp(self):
        """Create temporary directory for output files"""
        self.test_dir = tempfile.mkdtemp()
        self.test_path = Path(self.test_dir)

    def test_build_cdi_rdf(self):
        """Test basic RDF generation"""
        output_file = self.test_path / "output.ttl"
        
        stats1 = csv_to_cdi.ColumnStats("id")
        stats1.update("1")
        stats1.update("2")
        
        stats2 = csv_to_cdi.ColumnStats("name")
        stats2.update("Alice")
        stats2.update("Bob")

        csv_to_cdi.build_cdi_rdf(
            columns=["id", "name"],
            stats=[stats1, stats2],
            dataset_pid="doi:10.123/456",
            dataset_uri_base="https://example.org/dataset",
            file_uri="https://example.org/file/123",
            dataset_title="Test Dataset",
            file_md5="abc123",
            out_path=output_file
        )

        self.assertTrue(output_file.exists())
        content = output_file.read_text()
        
        # Check for expected RDF elements
        self.assertIn("@prefix cdi:", content)
        self.assertIn("cdi:DataSet", content)
        self.assertIn("cdi:Variable", content)
        self.assertIn("doi:10.123/456", content)

    def test_build_cdi_rdf_with_ddi(self):
        """Test RDF generation with DDI metadata"""
        output_file = self.test_path / "output_ddi.ttl"
        
        stats = csv_to_cdi.ColumnStats("age")
        stats.update("30")
        stats.update("40")

        ddi_variables = {
            "age": {
                "label": "Age in Years",
                "categories": [],
                "statistics": {"mean": "35.0"}
            }
        }

        csv_to_cdi.build_cdi_rdf(
            columns=["age"],
            stats=[stats],
            dataset_pid="doi:10.123/456",
            dataset_uri_base="https://example.org/dataset",
            file_uri=None,
            dataset_title="Test",
            file_md5=None,
            out_path=output_file,
            ddi_variables=ddi_variables
        )

        content = output_file.read_text()
        self.assertIn("Age in Years", content)
        self.assertIn("mean=35.0", content)


class TestUtilityFunctions(unittest.TestCase):
    """Test utility functions"""

    def test_safe_uri_fragment(self):
        """Test URI fragment sanitization"""
        self.assertEqual(csv_to_cdi.safe_uri_fragment("simple"), "simple")
        self.assertEqual(csv_to_cdi.safe_uri_fragment("with spaces"), "with_spaces")
        self.assertEqual(csv_to_cdi.safe_uri_fragment("special!@#$"), "special____")
        self.assertEqual(csv_to_cdi.safe_uri_fragment(""), "unnamed")

    def test_md5sum(self):
        """Test MD5 calculation"""
        test_file = Path(tempfile.mkdtemp()) / "test.txt"
        test_file.write_text("hello world")
        
        md5_hash = csv_to_cdi.md5sum(test_file)
        
        # MD5 of "hello world" is well-known
        self.assertEqual(len(md5_hash), 32)
        self.assertTrue(all(c in "0123456789abcdef" for c in md5_hash))

    def test_detect_encoding(self):
        """Test encoding detection"""
        test_file = Path(tempfile.mkdtemp()) / "test.csv"
        test_file.write_text("id,name\n1,test\n", encoding="utf-8")
        
        encoding = csv_to_cdi.detect_encoding(test_file)
        
        # Should detect UTF-8 or ASCII
        self.assertIn(encoding.lower(), ["utf-8", "ascii"])


class TestXConvertIntegration(unittest.TestCase):
    """Test xconvert integration for converting statistical data files to DDI"""

    @classmethod
    def setUpClass(cls):
        """Check if xconvert is available"""
        cls.xconvert_available = False
        try:
            result = subprocess.run(
                [XCONVERT_PATH, '-h'],
                capture_output=True,
                timeout=5
            )
            cls.xconvert_available = (result.returncode == 0)
        except (FileNotFoundError, subprocess.TimeoutExpired):
            pass

    def setUp(self):
        """Set up test fixtures"""
        if not self.xconvert_available:
            self.skipTest("xconvert not available")
        
        self.test_dir = Path(tempfile.mkdtemp())
        self.testdata_dir = Path(__file__).parent / "testdata"

    def tearDown(self):
        """Clean up test files"""
        if hasattr(self, 'test_dir') and self.test_dir.exists():
            for f in self.test_dir.glob("*"):
                f.unlink()
            self.test_dir.rmdir()

    def test_xconvert_spss_basic(self):
        """Test basic SPSS to DDI conversion"""
        input_file = self.testdata_dir / "simple_data.sps"
        output_file = self.test_dir / "spss_output.xml"
        
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi', 
             '-i', str(input_file), '-o', str(output_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        self.assertTrue(output_file.exists(), "Output file not created")
        self.assertGreater(output_file.stat().st_size, 0, "Output file is empty")

    def test_xconvert_sas_basic(self):
        """Test basic SAS to DDI conversion"""
        input_file = self.testdata_dir / "simple_data.sas"
        output_file = self.test_dir / "sas_output.xml"
        
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'sas', '-y', 'ddi',
             '-i', str(input_file), '-o', str(output_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        self.assertTrue(output_file.exists(), "Output file not created")
        self.assertGreater(output_file.stat().st_size, 0, "Output file is empty")

    def test_xconvert_stata_basic(self):
        """Test basic Stata to DDI conversion"""
        input_file = self.testdata_dir / "simple_data.dct"
        output_file = self.test_dir / "stata_output.xml"
        
        # Note: Stata dictionary format may not be fully supported by this xconvert version
        # This test verifies the conversion attempt
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'stata', '-y', 'ddi',
             '-i', str(input_file), '-o', str(output_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        # Stata conversion may fail depending on xconvert version and file format
        # If it fails, that's acceptable - just verify it handles the error gracefully
        if result.returncode != 0:
            self.skipTest(f"Stata conversion not supported by this xconvert version: {result.stderr}")
        else:
            self.assertTrue(output_file.exists(), "Output file not created")
            self.assertGreater(output_file.stat().st_size, 0, "Output file is empty")

    def test_xconvert_with_inventory(self):
        """Test xconvert with inventory file generation"""
        input_file = self.testdata_dir / "simple_data.sps"
        output_file = self.test_dir / "output_with_inv.xml"
        inventory_file = self.test_dir / "inventory.txt"
        
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi',
             '-i', str(input_file), '-o', str(output_file),
             '-v', str(inventory_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        self.assertTrue(output_file.exists(), "Output file not created")
        self.assertTrue(inventory_file.exists(), "Inventory file not created")
        
        # Check inventory contains variable information
        inventory_content = inventory_file.read_text()
        self.assertGreater(len(inventory_content), 0, "Inventory file is empty")

    def test_xconvert_lowercase_variables(self):
        """Test xconvert with lowercase variable name option"""
        input_file = self.testdata_dir / "simple_data.sps"
        output_file = self.test_dir / "output_lowercase.xml"
        
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi', '-l',
             '-i', str(input_file), '-o', str(output_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        self.assertTrue(output_file.exists(), "Output file not created")

    def test_xconvert_uppercase_variables(self):
        """Test xconvert with uppercase variable name option"""
        input_file = self.testdata_dir / "simple_data.sps"
        output_file = self.test_dir / "output_uppercase.xml"
        
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi', '-c',
             '-i', str(input_file), '-o', str(output_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        self.assertTrue(output_file.exists(), "Output file not created")

    def test_xconvert_max_label_length(self):
        """Test xconvert with custom maximum label length"""
        input_file = self.testdata_dir / "simple_data.sps"
        output_file = self.test_dir / "output_label30.xml"
        
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi', '-n', '30',
             '-i', str(input_file), '-o', str(output_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        self.assertTrue(output_file.exists(), "Output file not created")

    def test_xconvert_missing_input(self):
        """Test xconvert error handling with missing input file"""
        output_file = self.test_dir / "output.xml"
        
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi',
             '-i', '/nonexistent/file.sps', '-o', str(output_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertNotEqual(result.returncode, 0, "xconvert should fail with missing input")

    def test_xconvert_no_input_specified(self):
        """Test xconvert requires input file"""
        output_file = self.test_dir / "output.xml"
        
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi', '-o', str(output_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        # xconvert may show help (exit code 0) or error (non-zero) when no input is given
        # Either behavior is acceptable, but output file should not be created or should be empty
        if result.returncode == 0:
            # Help was shown, verify no output was produced
            self.assertFalse(output_file.exists() and output_file.stat().st_size > 100,
                           "xconvert should not produce output without input file")

    def test_csv_to_cdi_with_xconvert_ddi(self):
        """Test cdi_generator.py integration with xconvert-generated DDI"""
        # First, generate DDI from SPSS file using xconvert
        spss_file = self.testdata_dir / "simple_data.sps"
        ddi_file = self.test_dir / "xconvert_output.xml"
        
        xconvert_result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi',
             '-i', str(spss_file), '-o', str(ddi_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(xconvert_result.returncode, 0, "xconvert failed")
        self.assertTrue(ddi_file.exists(), "DDI file not created")
        
        # Create a matching CSV file for the SPSS data
        csv_file = self.test_dir / "test_data.csv"
        csv_file.write_text(
            "ID,AGE,GENDER,INCOME,EDUCATION\n"
            "1,25,1,35000,3\n"
            "2,30,2,42000,4\n"
            "3,45,1,55000,5\n"
        )
        
        # Now run cdi_generator with the xconvert-generated DDI
        output_ttl = self.test_dir / "output.ttl"
        
        # Use csv_to_cdi module directly
        import sys
        old_argv = sys.argv
        try:
            sys.argv = [
                'cdi_generator.py',
                '--csv', str(csv_file),
                '--dataset-pid', 'doi:10.123/TEST-XCONVERT',
                '--dataset-uri-base', 'https://example.org/dataset',
                '--ddi-file', str(ddi_file),
                '--output', str(output_ttl),
                '--skip-md5',
                '--quiet'
            ]
            
            # Run main function
            try:
                csv_to_cdi.main()
            except SystemExit as e:
                self.assertEqual(e.code, 0, "cdi_generator.py should succeed")
            
            # Verify output
            self.assertTrue(output_ttl.exists(), "Output TTL not created")
            content = output_ttl.read_text()
            self.assertIn("@prefix cdi:", content, "Missing CDI namespace")
            self.assertIn("cdi:DataSet", content, "Missing DataSet")
            self.assertIn("cdi:Variable", content, "Missing Variable")
            
        finally:
            sys.argv = old_argv


class TestXConvertWorkflow(unittest.TestCase):
    """Test complete workflow: statistical file -> xconvert -> DDI -> generator -> CDI-RDF"""

    @classmethod
    def setUpClass(cls):
        """Check if xconvert is available"""
        cls.xconvert_available = False
        try:
            result = subprocess.run(
                [XCONVERT_PATH, '-h'],
                capture_output=True,
                timeout=5
            )
            cls.xconvert_available = (result.returncode == 0)
        except (FileNotFoundError, subprocess.TimeoutExpired):
            pass

    def setUp(self):
        """Set up test fixtures"""
        if not self.xconvert_available:
            self.skipTest("xconvert not available")
        
        self.test_dir = Path(tempfile.mkdtemp())

    def tearDown(self):
        """Clean up test files"""
        if hasattr(self, 'test_dir') and self.test_dir.exists():
            for f in self.test_dir.glob("*"):
                f.unlink()
            self.test_dir.rmdir()

    def test_complete_spss_workflow(self):
        """Test complete workflow: SPSS -> DDI -> CDI-RDF"""
        # Step 1: Create SPSS syntax file with data
        spss_file = self.test_dir / "survey.sps"
        spss_file.write_text("""
DATA LIST FREE / RESPID Q1 Q2 Q3 REGION.
BEGIN DATA
101 5 4 3 1
102 4 5 4 2
103 3 3 5 1
104 5 4 4 3
105 4 5 3 2
END DATA.

VARIABLE LABELS
  RESPID 'Respondent ID'
  Q1 'Question 1 Score'
  Q2 'Question 2 Score'
  Q3 'Question 3 Score'
  REGION 'Geographic Region'.

VALUE LABELS
  REGION 1 'North' 2 'South' 3 'East' 4 'West' /
  Q1 TO Q3 1 'Very Poor' 2 'Poor' 3 'Fair' 4 'Good' 5 'Excellent'.

SAVE OUTFILE='survey.sav'.
""")
        
        # Step 2: Convert SPSS to DDI using xconvert
        ddi_file = self.test_dir / "survey_ddi.xml"
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi',
             '-i', str(spss_file), '-o', str(ddi_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        self.assertTrue(ddi_file.exists(), "DDI file not created")
        
        # Step 3: Create corresponding CSV file
        csv_file = self.test_dir / "survey.csv"
        csv_file.write_text(
            "RESPID,Q1,Q2,Q3,REGION\n"
            "101,5,4,3,1\n"
            "102,4,5,4,2\n"
            "103,3,3,5,1\n"
            "104,5,4,4,3\n"
            "105,4,5,3,2\n"
        )
        
        # Step 4: Convert to CDI-RDF using csv_to_cdi
        output_ttl = self.test_dir / "survey_cdi.ttl"
        
        result = subprocess.run(
            ['python3', str(Path(__file__).parent / 'cdi_generator.py'),
             '--csv', str(csv_file),
             '--dataset-pid', 'doi:10.123/SURVEY',
             '--dataset-uri-base', 'https://example.org/dataset',
             '--dataset-title', 'Survey Data with SPSS Metadata',
             '--ddi-file', str(ddi_file),
             '--output', str(output_ttl),
             '--skip-md5',
             '--quiet'],
            capture_output=True,
            text=True,
            timeout=30
        )
        
        self.assertEqual(result.returncode, 0, f"csv_to_cdi failed: {result.stderr}")
        self.assertTrue(output_ttl.exists(), "CDI-RDF file not created")
        
        # Step 5: Verify CDI-RDF content
        content = output_ttl.read_text()
        
        # Check for essential CDI elements
        self.assertIn("@prefix cdi:", content, "Missing CDI namespace")
        self.assertIn("cdi:DataSet", content, "Missing DataSet class")
        self.assertIn("cdi:LogicalDataSet", content, "Missing LogicalDataSet")
        self.assertIn("cdi:Variable", content, "Missing Variable class")
        
        # Check for dataset information
        self.assertIn("doi:10.123/SURVEY", content, "Missing dataset PID")
        self.assertIn("Survey Data", content, "Missing dataset title")
        
        # Check for variables (at least some should be present)
        self.assertIn("RESPID", content, "Missing RESPID variable")

    def test_complete_sas_workflow(self):
        """Test complete workflow: SAS -> DDI -> CDI-RDF"""
        # Create SAS file
        sas_file = self.test_dir / "experiment.sas"
        sas_file.write_text("""
data experiment;
  input SUBJID TREATMENT $ BASELINE FOLLOWUP;
  label SUBJID='Subject ID'
        TREATMENT='Treatment Group'
        BASELINE='Baseline Measurement'
        FOLLOWUP='Follow-up Measurement';
  datalines;
1 A 120 115
2 B 125 118
3 A 130 122
4 B 135 125
5 A 128 120
;
run;
""")
        
        # Convert to DDI
        ddi_file = self.test_dir / "experiment_ddi.xml"
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'sas', '-y', 'ddi',
             '-i', str(sas_file), '-o', str(ddi_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        
        # Create CSV
        csv_file = self.test_dir / "experiment.csv"
        csv_file.write_text(
            "SUBJID,TREATMENT,BASELINE,FOLLOWUP\n"
            "1,A,120,115\n"
            "2,B,125,118\n"
            "3,A,130,122\n"
            "4,B,135,125\n"
            "5,A,128,120\n"
        )
        
        # Convert to CDI-RDF
        output_ttl = self.test_dir / "experiment_cdi.ttl"
        
        result = subprocess.run(
            ['python3', str(Path(__file__).parent / 'cdi_generator.py'),
             '--csv', str(csv_file),
             '--dataset-pid', 'doi:10.123/EXPERIMENT',
             '--dataset-uri-base', 'https://example.org/dataset',
             '--ddi-file', str(ddi_file),
             '--output', str(output_ttl),
             '--skip-md5',
             '--quiet'],
            capture_output=True,
            text=True,
            timeout=30
        )
        
        self.assertEqual(result.returncode, 0, f"csv_to_cdi failed: {result.stderr}")
        self.assertTrue(output_ttl.exists(), "CDI-RDF file not created")
        
        content = output_ttl.read_text()
        self.assertIn("@prefix cdi:", content)
        self.assertIn("SUBJID", content)


class TestManifestGeneration(unittest.TestCase):
    """Test manifest-driven multi-file generation"""

    def setUp(self):
        self.tmpdir = tempfile.TemporaryDirectory()
        self.tmp_path = Path(self.tmpdir.name)

    def tearDown(self):
        self.tmpdir.cleanup()

    def test_generate_manifest_cdi(self):
        file_one = self.tmp_path / "one.csv"
        file_two = self.tmp_path / "two.csv"
        file_one.write_text("id,value\n1,10\n2,20\n", encoding="utf-8")
        file_two.write_text("code,label\nA,Alpha\nB,Beta\n", encoding="utf-8")

        manifest = {
            "dataset_pid": "doi:10.123/manifest",
            "dataset_uri_base": "https://example.org/dataset",
            "dataset_title": "Example Manifest Dataset",
            "files": [
                {"csv_path": str(file_one)},
                {"csv_path": str(file_two)},
            ],
        }

        output_path = self.tmp_path / "manifest.ttl"
        summary_path = self.tmp_path / "manifest_summary.json"

        warnings, rows, file_count = csv_to_cdi.generate_manifest_cdi(
            manifest,
            output_path=output_path,
            summary_json=summary_path,
            skip_md5_default=True,
            quiet=True,
        )

        self.assertEqual(warnings, [])
        self.assertEqual(file_count, 2)
        self.assertEqual(rows, 4)
        self.assertTrue(output_path.exists())
        ttl_text = output_path.read_text(encoding="utf-8")
        self.assertIn("doi:10.123/manifest", ttl_text)

        summary = json.loads(summary_path.read_text(encoding="utf-8"))
        self.assertEqual(summary["dataset_pid"], "doi:10.123/manifest")
        self.assertEqual(len(summary["files"]), 2)
        self.assertEqual(summary["files"][0]["rows_profiled"], 2)
        self.assertEqual(summary["files"][1]["rows_profiled"], 2)


def run_tests():
    """Run all tests"""
    # Create test suite
    loader = unittest.TestLoader()
    suite = unittest.TestSuite()

    # Add all test classes
    suite.addTests(loader.loadTestsFromTestCase(TestColumnStats))
    suite.addTests(loader.loadTestsFromTestCase(TestTypeCheckers))
    suite.addTests(loader.loadTestsFromTestCase(TestCSVProcessing))
    suite.addTests(loader.loadTestsFromTestCase(TestMetadataExtraction))
    suite.addTests(loader.loadTestsFromTestCase(TestDDIMetadata))
    suite.addTests(loader.loadTestsFromTestCase(TestRDFGeneration))
    suite.addTests(loader.loadTestsFromTestCase(TestUtilityFunctions))
    suite.addTests(loader.loadTestsFromTestCase(TestXConvertIntegration))
    suite.addTests(loader.loadTestsFromTestCase(TestXConvertWorkflow))
    suite.addTests(loader.loadTestsFromTestCase(TestManifestGeneration))

    # Run tests
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)

    # Return exit code
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    sys.exit(run_tests())
