#!/usr/bin/env python3
"""
Comprehensive test suite for cdi_generator_jsonld.py

Tests cover:
- CSV parsing and type inference
- DDI metadata integration
- Dataset metadata extraction
- JSON-LD output generation
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

# Add parent directory to path to import cdi_generator_jsonld
sys.path.insert(0, str(Path(__file__).parent))

import cdi_generator_jsonld as cdi_gen

# Path to xconvert binary
XCONVERT_PATH = os.environ.get('XCONVERT_PATH', '/usr/local/bin/xconvert')


class TestColumnStats(unittest.TestCase):
    """Test column statistics and type inference"""

    def test_int_inference(self):
        """Test integer type detection"""
        stats = cdi_gen.ColumnStats("test_col")
        stats.update("1")
        stats.update("2")
        stats.update("3")
        self.assertEqual(stats.xsd_datatype_name(), "integer")

    def test_float_inference(self):
        """Test float type detection"""
        stats = cdi_gen.ColumnStats("test_col")
        stats.update("1.5")
        stats.update("2.7")
        stats.update("3.14")
        self.assertEqual(stats.xsd_datatype_name(), "decimal")

    def test_bool_inference(self):
        """Test boolean type detection"""
        stats = cdi_gen.ColumnStats("test_col")
        stats.update("true")
        stats.update("false")
        stats.update("true")
        self.assertEqual(stats.xsd_datatype_name(), "boolean")

    def test_datetime_inference(self):
        """Test datetime type detection"""
        stats = cdi_gen.ColumnStats("test_col")
        stats.update("2023-01-01")
        stats.update("2023-12-31")
        self.assertEqual(stats.xsd_datatype_name(), "dateTime")

    def test_string_fallback(self):
        """Test string fallback for mixed types"""
        stats = cdi_gen.ColumnStats("test_col")
        stats.update("hello")
        stats.update("world")
        self.assertEqual(stats.xsd_datatype_name(), "string")

    def test_missing_values(self):
        """Test handling of missing values"""
        stats = cdi_gen.ColumnStats("test_col")
        stats.update("NA")
        stats.update("null")
        stats.update("")
        stats.update("1")
        stats.update("2")
        self.assertEqual(stats.n_non_missing, 2)
        self.assertEqual(stats.xsd_datatype_name(), "integer")

    def test_role_identifier(self):
        """Test identifier role detection (high uniqueness)"""
        stats = cdi_gen.ColumnStats("id")
        for i in range(100):
            stats.update(str(i))
        self.assertEqual(stats.role(), "identifier")

    def test_role_measure(self):
        """Test measure role detection (numeric, low uniqueness)"""
        stats = cdi_gen.ColumnStats("score")
        for i in range(100):
            stats.update(str(i % 10))  # Only 10 unique values
        self.assertEqual(stats.role(), "measure")

    def test_role_dimension(self):
        """Test dimension role detection (categorical)"""
        stats = cdi_gen.ColumnStats("category")
        for i in range(100):
            stats.update(["A", "B", "C"][i % 3])
        self.assertEqual(stats.role(), "dimension")

    def test_ddi_datatype_uri(self):
        """Test DDI CV datatype URI generation"""
        stats = cdi_gen.ColumnStats("test_col")
        stats.update("123")
        self.assertIn("Integer", stats.ddi_datatype_uri())


class TestTypeCheckers(unittest.TestCase):
    """Test helper functions for type checking"""

    def test_is_int(self):
        """Test integer detection"""
        self.assertTrue(cdi_gen.is_int("123"))
        self.assertTrue(cdi_gen.is_int("-456"))
        self.assertTrue(cdi_gen.is_int("  789  "))
        self.assertFalse(cdi_gen.is_int("123.45"))
        self.assertFalse(cdi_gen.is_int("abc"))

    def test_is_float(self):
        """Test float detection"""
        self.assertTrue(cdi_gen.is_float("123.45"))
        self.assertTrue(cdi_gen.is_float("-67.89"))
        self.assertFalse(cdi_gen.is_float("123"))  # Integers excluded
        self.assertFalse(cdi_gen.is_float("abc"))

    def test_is_bool(self):
        """Test boolean detection"""
        self.assertTrue(cdi_gen.is_bool("true"))
        self.assertTrue(cdi_gen.is_bool("FALSE"))
        self.assertTrue(cdi_gen.is_bool("yes"))
        self.assertTrue(cdi_gen.is_bool("0"))
        self.assertTrue(cdi_gen.is_bool("1"))
        self.assertFalse(cdi_gen.is_bool("maybe"))

    def test_is_datetime(self):
        """Test datetime detection"""
        self.assertTrue(cdi_gen.is_datetime("2023-01-01"))
        self.assertTrue(cdi_gen.is_datetime("01/15/2023"))
        self.assertTrue(cdi_gen.is_datetime("2023-12-31T23:59:59"))
        self.assertFalse(cdi_gen.is_datetime("not a date"))


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

        cols, stats, info, md5, dialect = cdi_gen.stream_profile_csv(
            csv_file, header=True, compute_md5=False
        )

        self.assertEqual(len(cols), 3)
        self.assertEqual(cols, ["id", "name", "age"])
        self.assertEqual(info["rows_read"], 2)
        self.assertEqual(stats[0].xsd_datatype_name(), "integer")  # id
        self.assertEqual(stats[1].xsd_datatype_name(), "string")   # name
        self.assertEqual(stats[2].xsd_datatype_name(), "integer")  # age

    def test_csv_with_missing_values(self):
        """Test CSV with missing/null values"""
        csv_file = self.test_path / "missing.csv"
        csv_file.write_text("id,value\n1,100\n2,NA\n3,null\n4,200\n")

        cols, stats, info, _, _ = cdi_gen.stream_profile_csv(
            csv_file, compute_md5=False
        )

        # Should have 2 non-missing values for 'value' column
        self.assertEqual(stats[1].n_non_missing, 2)
        self.assertEqual(stats[1].xsd_datatype_name(), "integer")

    def test_csv_no_header(self):
        """Test CSV without header row"""
        csv_file = self.test_path / "no_header.csv"
        csv_file.write_text("1,John,30\n2,Jane,25\n")

        cols, stats, info, _, _ = cdi_gen.stream_profile_csv(
            csv_file, header=False, compute_md5=False
        )

        # Should auto-generate column names
        self.assertEqual(cols, ["col_1", "col_2", "col_3"])
        self.assertEqual(info["rows_read"], 2)

    def test_header_auto_detection_on_dataverse_tab(self):
        """Ensure auto header detection does not treat data row as header."""
        csv_file = self.test_path / "dataverse_no_header.tab"
        csv_file.write_text("1\t2020-01-15\t95.5\tJohn Doe\n2\t2021-03-22\t88.2\tJane Smith\n")

        cols, stats, info, _, _ = cdi_gen.stream_profile_csv(
            csv_file, delimiter="\t", header="auto", compute_md5=False
        )

        self.assertEqual(cols, ["col_1", "col_2", "col_3", "col_4"])
        self.assertEqual(info["rows_read"], 2)
        # First column should be treated as integer measure rather than identifier string
        self.assertEqual(stats[0].xsd_datatype_name(), "integer")

    def test_custom_delimiter(self):
        """Test CSV with custom delimiter"""
        csv_file = self.test_path / "semicolon.csv"
        csv_file.write_text("id;name;age\n1;John;30\n2;Jane;25\n")

        cols, stats, info, _, dialect = cdi_gen.stream_profile_csv(
            csv_file, delimiter=";", header=True, compute_md5=False
        )

        self.assertEqual(len(cols), 3)
        self.assertEqual(dialect.delimiter, ";")

    def test_row_limit(self):
        """Test limiting rows processed"""
        csv_file = self.test_path / "large.csv"
        lines = ["id,value\n"] + [f"{i},{i*10}\n" for i in range(1000)]
        csv_file.write_text("".join(lines))

        cols, stats, info, _, _ = cdi_gen.stream_profile_csv(
            csv_file, limit_rows=100, compute_md5=False
        )

        self.assertEqual(info["rows_read"], 100)

    def test_empty_csv(self):
        """Test error handling for empty CSV"""
        csv_file = self.test_path / "empty.csv"
        csv_file.write_text("")

        with self.assertRaises(ValueError):
            cdi_gen.stream_profile_csv(csv_file, compute_md5=False)

    def test_md5_calculation(self):
        """Test MD5 hash calculation"""
        csv_file = self.test_path / "test.csv"
        csv_file.write_text("id,name\n1,test\n")

        _, _, _, md5_hash, _ = cdi_gen.stream_profile_csv(
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

        title = cdi_gen.extract_dataset_title(metadata)
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

        title = cdi_gen.extract_dataset_title(metadata)
        self.assertEqual(title, "Test Dataset")

    def test_extract_dataset_description(self):
        """Test extracting dataset description"""
        metadata = {
            "datasetVersion": {
                "metadataBlocks": {
                    "citation": {
                        "fields": [
                            {"typeName": "dsDescription", "value": [
                                {"dsDescriptionValue": {"value": "A test description"}}
                            ]}
                        ]
                    }
                }
            }
        }

        description = cdi_gen.extract_dataset_description(metadata)
        self.assertEqual(description, "A test description")


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

        raw_xml, variables, is_xml = cdi_gen.load_ddi_metadata(ddi_file)

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

        raw_xml, variables, is_xml = cdi_gen.load_ddi_metadata(ddi_file)

        # Should return raw text but no parsed variables
        self.assertIsNotNone(raw_xml)
        self.assertEqual(len(variables), 0)
        self.assertFalse(is_xml)

    def test_load_ddi_metadata_fixture(self):
        """Test loading the real GetDataFileDDI fixture."""
        ddi_path = Path(__file__).parent / "testdata" / "tmp_ddi8.xml"
        if not ddi_path.exists():
            self.skipTest("DDI fixture tmp_ddi8.xml not found")

        raw_xml, variables, is_xml = cdi_gen.load_ddi_metadata(ddi_path)

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


class TestJSONLDGeneration(unittest.TestCase):
    """Test JSON-LD generation"""

    def setUp(self):
        """Create temporary directory for output files"""
        self.test_dir = tempfile.mkdtemp()
        self.test_path = Path(self.test_dir)

    def test_build_jsonld_graph(self):
        """Test basic JSON-LD generation"""
        stats1 = cdi_gen.ColumnStats("id")
        stats1.update("1")
        stats1.update("2")
        
        stats2 = cdi_gen.ColumnStats("name")
        stats2.update("Alice")
        stats2.update("Bob")

        files_data = [{
            "file_name": "test.csv",
            "columns": ["id", "name"],
            "stats": [stats1, stats2],
            "ddi_variables": {},
        }]

        jsonld_doc = cdi_gen.build_jsonld_graph(
            dataset_title="Test Dataset",
            dataset_description="A test description",
            files_data=files_data,
            dataset_pid="doi:10.123/456",
        )

        # Check for expected JSON-LD structure
        self.assertIn("@context", jsonld_doc)
        self.assertEqual(jsonld_doc["@context"], cdi_gen.DDI_CDI_CONTEXT)
        self.assertIn("@graph", jsonld_doc)
        
        graph = jsonld_doc["@graph"]
        types = [node.get("@type") for node in graph]
        
        # Check for expected DDI-CDI types
        self.assertTrue(any("WideDataSet" in str(t) for t in types))
        self.assertTrue(any("WideDataStructure" in str(t) for t in types))
        self.assertTrue(any("InstanceVariable" in str(t) or "RepresentedVariable" in str(t) for t in types))

    def test_build_jsonld_graph_with_ddi(self):
        """Test JSON-LD generation with DDI metadata"""
        stats = cdi_gen.ColumnStats("age")
        stats.update("30")
        stats.update("40")

        ddi_variables = {
            "age": {
                "label": "Age in Years",
                "categories": [],
                "statistics": {"mean": "35.0"}
            }
        }

        files_data = [{
            "file_name": "test.csv",
            "columns": ["age"],
            "stats": [stats],
            "ddi_variables": ddi_variables,
        }]

        jsonld_doc = cdi_gen.build_jsonld_graph(
            dataset_title="Test",
            dataset_description=None,
            files_data=files_data,
            dataset_pid="doi:10.123/456",
        )

        # Find the variable node
        graph = jsonld_doc["@graph"]
        var_nodes = [n for n in graph if "InstanceVariable" in str(n.get("@type", []))]
        
        self.assertTrue(len(var_nodes) > 0)
        # Variable should use DDI label
        var_node = var_nodes[0]
        self.assertEqual(var_node.get("name"), "Age in Years")

    def test_jsonld_property_names_for_shacl_compliance(self):
        """Test JSON-LD uses correct property names for DDI-CDI 1.0 context.
        
        This ensures generated JSON-LD passes SHACL validation:
        - ValueMappingPosition uses "indexes" not "indexes_ValueMapping"
        - PhysicalSegmentLayout has separate has_ValueMapping and has_ValueMappingPosition
        """
        stats1 = cdi_gen.ColumnStats("id")
        stats1.update("1")
        stats1.update("2")
        
        stats2 = cdi_gen.ColumnStats("name")
        stats2.update("Alice")
        stats2.update("Bob")

        files_data = [{
            "file_name": "test.csv",
            "columns": ["id", "name"],
            "stats": [stats1, stats2],
            "ddi_variables": {},
        }]

        jsonld_doc = cdi_gen.build_jsonld_graph(
            dataset_title="Test Dataset",
            dataset_description="A test description",
            files_data=files_data,
            dataset_pid="doi:10.123/456",
        )

        graph = jsonld_doc["@graph"]
        
        # Find PhysicalSegmentLayout
        psl_nodes = [n for n in graph if n.get("@type") == "PhysicalSegmentLayout"]
        self.assertEqual(len(psl_nodes), 1)
        psl = psl_nodes[0]
        
        # PhysicalSegmentLayout must have separate properties
        self.assertIn("has_ValueMapping", psl,
            "PhysicalSegmentLayout must have has_ValueMapping")
        self.assertIn("has_ValueMappingPosition", psl,
            "PhysicalSegmentLayout must have has_ValueMappingPosition")
        
        # Verify they're lists with the right number of items
        self.assertIsInstance(psl["has_ValueMapping"], list)
        self.assertIsInstance(psl["has_ValueMappingPosition"], list)
        self.assertEqual(len(psl["has_ValueMapping"]), 2)  # 2 columns
        self.assertEqual(len(psl["has_ValueMappingPosition"]), 2)
        
        # Find ValueMappingPosition nodes
        vmp_nodes = [n for n in graph if n.get("@type") == "ValueMappingPosition"]
        self.assertEqual(len(vmp_nodes), 2)  # 2 columns
        
        for vmp in vmp_nodes:
            # Must use "indexes" not "indexes_ValueMapping"
            self.assertIn("indexes", vmp,
                "ValueMappingPosition must use 'indexes' property")
            self.assertNotIn("indexes_ValueMapping", vmp,
                "ValueMappingPosition should NOT use 'indexes_ValueMapping'")


class TestUtilityFunctions(unittest.TestCase):
    """Test utility functions"""

    def test_safe_fragment(self):
        """Test URI fragment sanitization"""
        self.assertEqual(cdi_gen.safe_fragment("simple"), "simple")
        self.assertEqual(cdi_gen.safe_fragment("with spaces"), "with_spaces")
        self.assertEqual(cdi_gen.safe_fragment("special!@#$"), "special____")
        self.assertEqual(cdi_gen.safe_fragment(""), "unnamed")

    def test_md5sum(self):
        """Test MD5 calculation"""
        test_file = Path(tempfile.mkdtemp()) / "test.txt"
        test_file.write_text("hello world")
        
        md5_hash = cdi_gen.md5sum(test_file)
        
        # MD5 of "hello world" is well-known
        self.assertEqual(len(md5_hash), 32)
        self.assertTrue(all(c in "0123456789abcdef" for c in md5_hash))

    def test_detect_encoding(self):
        """Test encoding detection"""
        test_file = Path(tempfile.mkdtemp()) / "test.csv"
        test_file.write_text("id,name\n1,test\n", encoding="utf-8")
        
        encoding = cdi_gen.detect_encoding(test_file)
        
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
        if not input_file.exists():
            self.skipTest("SPSS test file not found")
            
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
        if not input_file.exists():
            self.skipTest("SAS test file not found")
            
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


class TestXConvertWorkflow(unittest.TestCase):
    """Test complete workflow: statistical file -> xconvert -> DDI -> generator -> CDI JSON-LD"""

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
        """Test complete workflow: SPSS -> DDI -> CDI JSON-LD"""
        testdata_dir = Path(__file__).parent / "testdata"
        input_file = testdata_dir / "simple_data.sps"
        if not input_file.exists():
            self.skipTest("SPSS test file not found")
        
        # Step 1: Convert SPSS to DDI using xconvert
        ddi_file = self.test_dir / "survey_ddi.xml"
        result = subprocess.run(
            [XCONVERT_PATH, '-x', 'spss', '-y', 'ddi',
             '-i', str(input_file), '-o', str(ddi_file)],
            capture_output=True,
            text=True,
            timeout=10
        )
        
        self.assertEqual(result.returncode, 0, f"xconvert failed: {result.stderr}")
        self.assertTrue(ddi_file.exists(), "DDI file not created")
        
        # Step 2: Create corresponding CSV file
        csv_file = self.test_dir / "survey.csv"
        csv_file.write_text(
            "RESPID,Q1,Q2,Q3,REGION\n"
            "101,5,4,3,1\n"
            "102,4,5,4,2\n"
            "103,3,3,5,1\n"
            "104,5,4,4,3\n"
            "105,4,5,3,2\n"
        )
        
        # Step 3: Convert to CDI JSON-LD
        output_jsonld = self.test_dir / "survey_cdi.jsonld"
        
        result = subprocess.run(
            ['python3', str(Path(__file__).parent / 'cdi_generator_jsonld.py'),
             '--csv', str(csv_file),
             '--dataset-pid', 'doi:10.123/SURVEY',
             '--dataset-uri-base', 'https://example.org/dataset',
             '--dataset-title', 'Survey Data with SPSS Metadata',
             '--ddi-file', str(ddi_file),
             '--output', str(output_jsonld),
             '--skip-md5',
             '--quiet'],
            capture_output=True,
            text=True,
            timeout=30
        )
        
        self.assertEqual(result.returncode, 0, f"cdi_generator_jsonld failed: {result.stderr}")
        self.assertTrue(output_jsonld.exists(), "CDI JSON-LD file not created")
        
        # Step 4: Verify JSON-LD content
        content = json.loads(output_jsonld.read_text())
        
        # Check for essential JSON-LD elements
        self.assertIn("@context", content, "Missing @context")
        self.assertIn("@graph", content, "Missing @graph")
        
        # Check for DDI-CDI types
        types = [str(node.get("@type", "")) for node in content["@graph"]]
        self.assertTrue(any("WideDataSet" in t for t in types), "Missing WideDataSet")


class TestManifestGeneration(unittest.TestCase):
    """Test manifest-driven multi-file generation"""

    def setUp(self):
        self.tmpdir = tempfile.TemporaryDirectory()
        self.tmp_path = Path(self.tmpdir.name)

    def tearDown(self):
        self.tmpdir.cleanup()

    def test_generate_manifest_jsonld(self):
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

        output_path = self.tmp_path / "manifest.jsonld"
        summary_path = self.tmp_path / "manifest_summary.json"

        warnings, rows, file_count = cdi_gen.generate_manifest_jsonld(
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
        
        jsonld_content = json.loads(output_path.read_text(encoding="utf-8"))
        self.assertIn("@context", jsonld_content)
        self.assertIn("@graph", jsonld_content)

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
    suite.addTests(loader.loadTestsFromTestCase(TestJSONLDGeneration))
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
