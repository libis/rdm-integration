#!/usr/bin/env python3
# ----------------------------- requirements.txt -----------------------------
# rdflib==7.0.0
# chardet==5.2.0
# datasketch==1.5.9
# python-dateutil==2.9.0.post0
# ---------------------------------------------------------------------------

"""
CSV -> DDI-CDI RDF (Turtle) at FILE LEVEL, streaming & memory-safe.

- Streams huge CSVs row-by-row; never loads the whole file.
- Infers per-column XSD datatype and a role (identifier/dimension/measure/attribute).
- Uses HyperLogLog (datasketch) to approximate distinct counts with tiny memory.
- Emits a minimal CDI profile as Turtle: DataSet, PhysicalDataSet, LogicalDataSet, Variables, ProcessStep.
- Keeps *link predicates* generic (dcterms:hasPart, etc.) to be easily swapped to exact CDI properties later.

USAGE
------
python csv_to_cdi.py \
  --csv /data/big.csv \
  --dataset-pid "doi:10.70122/FK2/EXAMPLE" \
  --dataset-uri-base "https://rdr.kuleuven.be/dataset" \
  --file-uri "https://rdr.kuleuven.be/api/access/datafile/123456" \
  --dataset-title "Example dataset" \
  --output dataset.cdi.ttl

Notes
-----
- Header is assumed on the first row by default; use --no-header to auto-name cols.
- Encoding is detected on a sample via chardet; override with --encoding if needed.
- Delimiter is sniffed unless provided with --delimiter.
- For gz files, pass the decompressed path (or pipe through zcat). Keeping it simple avoids double-reading.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import logging
import sys
from pathlib import Path
from typing import Any, List, Optional, Dict, Tuple
import xml.etree.ElementTree as ET
import chardet
from datasketch import HyperLogLog
from dateutil import parser as dateparser
from rdflib import Graph, Namespace, URIRef, BNode, Literal
from rdflib.namespace import RDF, DCTERMS, XSD


# ---- Core namespaces (DDI-CDI + vocabularies) ----
CDI = Namespace("http://www.ddialliance.org/Specification/DDI-CDI/1.0/RDF/")
PROV = Namespace("http://www.w3.org/ns/prov#")
SKOS = Namespace("http://www.w3.org/2004/02/skos/core#")


# ---- Link predicates (Phase 1: native CDI). If the profile changes, swap to FALLBACK below. ----
NATIVE_LINKS = {
    "dataset_to_logical": CDI.hasLogicalDataSet,   # native CDI: DataSet -> LogicalDataSet
    "dataset_to_physical": CDI.hasPhysicalDataSet, # native CDI: DataSet -> PhysicalDataSet
    "logical_to_variable": CDI.containsVariable,   # native CDI: LogicalDataSet -> Variable
    "variable_to_role":    CDI.hasRole,            # native CDI: Variable -> Role
    "variable_to_repr":    CDI.hasRepresentation,  # native CDI: Variable -> Representation
}

# ---- Fallback (generic) mapping, if a different application profile is agreed later ----
FALLBACK_LINKS = {
    "dataset_to_logical": DCTERMS.hasPart,     # generic containment
    "dataset_to_physical": DCTERMS.hasPart,    # generic containment
    "logical_to_variable": DCTERMS.hasPart,    # generic containment
    "variable_to_role":    DCTERMS.type,       # descriptive type-as-role (temporary)
    "variable_to_repr":    DCTERMS.conformsTo, # representation as a standard conformance (temporary)
}

# Choose which to use (swap if the profile changes)
ACTIVE_LINKS = NATIVE_LINKS
LINK = ACTIVE_LINKS


# -------------------------- Streaming type inference --------------------------

MISSING = {"", "na", "n/a", "null", "none", "nan", "NA", "N/A", "NULL", "None", "NaN"}

def is_int(s: str) -> bool:
    """Check if string represents an integer."""
    try:
        # int() handles leading/trailing spaces
        int(s)
        return True
    except (ValueError, TypeError):
        return False

def is_float(s: str) -> bool:
    """Check if string represents a float (but not an integer)."""
    try:
        float(s)
        # Exclude ints that parse as float without decimal to keep int distinct
        return not is_int(s)
    except (ValueError, TypeError):
        return False

def is_bool(s: str) -> bool:
    """Check if string represents a boolean value."""
    return s.lower() in {"true", "false", "t", "f", "0", "1", "yes", "no", "y", "n"}

def is_datetime(s: str) -> bool:
    """Check if string represents a datetime value."""
    try:
        # robust, but still fast enough for sampling
        dateparser.parse(s, fuzzy=False)
        return True
    except (ValueError, TypeError, OverflowError):
        return False


class ColumnStats:
    """
    Holds streaming stats for a single column to infer:
      - xsd datatype (integer/decimal/boolean/dateTime/string)
      - distinct count (approx via HLL)
      - role (identifier/dimension/measure/attribute)
    """

    __slots__ = (
        "name",
        "n_non_missing",
        "n_rows",
        "hll",
        "could_be_int",
        "could_be_float",
        "could_be_bool",
        "could_be_datetime",
    )

    def __init__(self, name: str):
        self.name = name
        self.n_non_missing = 0
        self.n_rows = 0
        self.hll = HyperLogLog(p=12)  # ~0.016 precision, ~1.5KB memory
        self.could_be_int = True
        self.could_be_float = True
        self.could_be_bool = True
        self.could_be_datetime = True

    def update(self, raw: Optional[str]):
        self.n_rows += 1
        if raw is None:
            return
        s = raw.strip()
        if s in MISSING:
            return
        self.n_non_missing += 1

        # HyperLogLog for approximate distinct
        self.hll.update(s.encode("utf-8", "ignore"))

        # Narrow candidate types
        if self.could_be_int and not is_int(s):
            self.could_be_int = False

        if self.could_be_float and not (is_float(s) or is_int(s)):
            self.could_be_float = False

        if self.could_be_bool and not is_bool(s):
            self.could_be_bool = False

        if self.could_be_datetime and not is_datetime(s):
            self.could_be_datetime = False

    def xsd_datatype(self):
        # Priority: int > decimal > boolean > dateTime > string
        if self.could_be_int and self.n_non_missing > 0:
            return XSD.integer
        if self.could_be_float and self.n_non_missing > 0:
            return XSD.decimal
        if self.could_be_bool and self.n_non_missing > 0:
            return XSD.boolean
        if self.could_be_datetime and self.n_non_missing > 0:
            return XSD.dateTime
        return XSD.string

    def approx_distinct(self) -> int:
        return int(self.hll.count())

    def role(self) -> str:
        """
        Heuristic role inference:
          - identifier: ~unique column (>= 95% distinct among non-missing)
          - measure: numeric (int/decimal) non-unique
          - dimension: low cardinality text or boolean
          - attribute: everything else
        """
        if self.n_non_missing == 0:
            return "attribute"

        distinct = self.approx_distinct()
        uniq_ratio = distinct / max(1, self.n_non_missing)

        if uniq_ratio >= 0.95 and self.n_non_missing >= 50:
            return "identifier"

        dt = self.xsd_datatype()
        if dt in (XSD.integer, XSD.decimal) and uniq_ratio < 0.95:
            return "measure"

        if dt in (XSD.boolean,) or distinct <= min(50, int(0.1 * self.n_non_missing)):
            return "dimension"

        return "attribute"


# ------------------------------ CSV streaming core ------------------------------

def setup_logging(verbose: bool = False, quiet: bool = False) -> None:
    """Set up logging configuration."""
    if quiet:
        level = logging.WARNING
    else:
        level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format='%(asctime)s - %(levelname)s - %(message)s',
        datefmt='%Y-%m-%d %H:%M:%S'
    )

def detect_encoding(path: Path, sample_bytes: int = 1024 * 1024) -> str:
    """Detect file encoding using chardet."""
    logging.info(f"Detecting encoding for {path}")
    try:
        with path.open("rb") as f:
            raw = f.read(sample_bytes)
        res = chardet.detect(raw)
        encoding = (res.get("encoding") or "utf-8").lower()
        confidence = res.get("confidence", 0)
        logging.info(f"Detected encoding: {encoding} (confidence: {confidence:.2f})")
        return encoding
    except Exception as e:
        logging.warning(f"Error detecting encoding: {e}. Using UTF-8 as fallback.")
        return "utf-8"

def detect_dialect(path: Path, encoding: str, sample_bytes: int = 256 * 1024) -> csv.Dialect:
    """Detect CSV dialect (delimiter, quoting, etc.)."""
    logging.info("Detecting CSV dialect")
    try:
        with path.open("rb") as fb:
            raw = fb.read(sample_bytes)
        text = raw.decode(encoding, errors="replace")
        
        sniffer = csv.Sniffer()
        dialect = sniffer.sniff(text)
        logging.info(f"Detected delimiter: '{dialect.delimiter}', quotechar: '{dialect.quotechar}'")
        return dialect
    except Exception as e:
        logging.warning(f"Error detecting CSV dialect: {e}. Using defaults.")
        # Fallback to RFC4180-ish defaults
        class _D(csv.Dialect):
            delimiter = ","
            quotechar = '"'
            doublequote = True
            skipinitialspace = False
            lineterminator = "\n"
            quoting = csv.QUOTE_MINIMAL
        return _D()

def md5sum(path: Path, chunk: int = 1024 * 1024) -> str:
    """Calculate MD5 hash of file."""
    logging.info(f"Calculating MD5 hash for {path}")
    h = hashlib.md5()
    try:
        with path.open("rb") as f:
            for b in iter(lambda: f.read(chunk), b""):
                h.update(b)
        return h.hexdigest()
    except Exception as e:
        logging.error(f"Error calculating MD5 hash: {e}")
        return ""


def read_metadata_from_stdin() -> Optional[Dict[str, Any]]:
    """Attempt to load dataset metadata JSON from STDIN."""
    stream = sys.stdin
    if stream is None or stream.closed:
        return None
    try:
        if stream.isatty():
            return None
    except Exception:
        return None
    raw = stream.read()
    if not raw or not raw.strip():
        return None
    try:
        return json.loads(raw)
    except json.JSONDecodeError as exc:
        logging.warning("Failed to parse metadata from stdin: %s", exc)
        return None


def extract_dataset_title(metadata: Dict[str, Any]) -> Optional[str]:
    """Pull dataset title from Dataverse-style metadata."""
    dataset_version = metadata.get("datasetVersion") or metadata.get("latestVersion")
    if not isinstance(dataset_version, dict):
        return None
    metadata_blocks = dataset_version.get("metadataBlocks")
    if not isinstance(metadata_blocks, dict):
        return None
    citation_block = metadata_blocks.get("citation")
    if not isinstance(citation_block, dict):
        return None
    fields = citation_block.get("fields")
    if not isinstance(fields, list):
        return None
    for field in fields:
        if not isinstance(field, dict):
            continue
        if field.get("typeName") != "title":
            continue
        value = field.get("value")
        if isinstance(value, str):
            return value
        if isinstance(value, list) and value:
            first = value[0]
            if isinstance(first, str):
                return first
    return None


def extract_file_uri(metadata: Dict[str, Any], filename: str, base_url: str) -> Optional[str]:
    """Extract file URI from Dataverse metadata JSON by matching filename."""
    def collect_files(data: Any) -> List[Dict[str, Any]]:
        if not isinstance(data, dict):
            return []
        files = []
        # Try multiple locations where files might be
        for key in ["files", "datasetVersion", "latestVersion"]:
            value = data.get(key)
            if isinstance(value, dict):
                files.extend(collect_files(value))
            elif isinstance(value, list):
                for item in value:
                    if isinstance(item, dict):
                        # This is a file entry
                        if "dataFile" in item or "label" in item:
                            files.append(item)
        return files

    files = collect_files(metadata)
    for entry in files:
        label = entry.get("label", "")
        dir_label = entry.get("directoryLabel", "")
        relative = label
        if dir_label:
            relative = dir_label.rstrip("/") + "/" + label
        
        # Check if this matches our file
        if relative == filename or label == filename:
            datafile = entry.get("dataFile", {})
            if isinstance(datafile, dict):
                # Try pidURL first, then persistentId, then construct from id
                if pid_url := datafile.get("pidURL"):
                    return pid_url
                if persistent_id := datafile.get("persistentId"):
                    return persistent_id
                if file_id := datafile.get("id"):
                    return f"{base_url.rstrip('/')}/api/access/datafile/{file_id}"
        
        # Also check dataFile.filename
        datafile = entry.get("dataFile", {})
        if isinstance(datafile, dict) and datafile.get("filename") == filename:
            if pid_url := datafile.get("pidURL"):
                return pid_url
            if persistent_id := datafile.get("persistentId"):
                return persistent_id
            if file_id := datafile.get("id"):
                return f"{base_url.rstrip('/')}/api/access/datafile/{file_id}"
    
    return None


def load_metadata_from_file(path: Path) -> Optional[Dict[str, Any]]:
    """Load dataset metadata from a JSON file."""
    if not path.exists():
        return None
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        logging.warning("Failed to parse metadata from %s: %s", path, exc)
        return None


def _strip_ddi_tag(tag: str) -> str:
    return tag.split('}', 1)[1] if '}' in tag else tag


def load_ddi_metadata(ddi_path: Path) -> Tuple[Optional[str], Dict[str, Dict[str, Any]], bool]:
    """Read and parse a DDI fragment, returning sanitized XML (if valid) and per-variable metadata.

    The boolean flag indicates whether the returned string is well-formed XML and can safely be
    treated as an rdf:XMLLiteral.
    """
    try:
        raw_bytes = ddi_path.read_bytes()
    except OSError as exc:
        logging.warning("Failed to read DDI file %s: %s", ddi_path, exc)
        return None, {}, False

    raw_text = raw_bytes.decode("utf-8", errors="replace")
    try:
        root = ET.fromstring(raw_text)
    except ET.ParseError as exc:
        logging.warning("Failed to parse DDI XML from %s: %s", ddi_path, exc)
        return raw_text.strip(), {}, False

    # Re-serialize to strip XML declarations and ensure consistent formatting
    sanitized_xml = ET.tostring(root, encoding="unicode").strip()

    variables: Dict[str, Dict[str, Any]] = {}
    for var_elem in root.iter():
        if _strip_ddi_tag(var_elem.tag) != "var":
            continue
        name = var_elem.attrib.get("name")
        if not name:
            continue
        info: Dict[str, Any] = {
            "label": None,
            "categories": [],
            "statistics": {},
        }
        for child in list(var_elem):
            tag = _strip_ddi_tag(child.tag)
            if tag == "labl" and child.text:
                info["label"] = child.text.strip()
            elif tag == "sumStat":
                stat_type = child.attrib.get("type")
                if stat_type and child.text:
                    info["statistics"][stat_type] = child.text.strip()
            elif tag == "catgry":
                cat_value: Optional[str] = None
                cat_label: Optional[str] = None
                for cat_child in list(child):
                    cat_tag = _strip_ddi_tag(cat_child.tag)
                    if cat_tag == "catValu" and cat_child.text:
                        cat_value = cat_child.text.strip()
                    elif cat_tag == "labl" and cat_child.text:
                        cat_label = cat_child.text.strip()
                if cat_value:
                    info["categories"].append((cat_value, cat_label))
        variables[name] = info

    return sanitized_xml, variables, True


def stream_profile_csv(
    path: Path,
    encoding: Optional[str] = None,
    delimiter: Optional[str] = None,
    header: bool = True,
    limit_rows: Optional[int] = None,
    compute_md5: bool = True,
) -> Tuple[List[str], List[ColumnStats], Dict[str, int], Optional[str], csv.Dialect]:
    """
    Stream the CSV once and build column stats.
    Returns: (column_names, stats[], row_count_info, file_md5, dialect)
    file_md5 may be None when checksum calculation is skipped.
    """
    logging.info(f"Starting to profile CSV: {path}")
    
    # Validate input file
    if not path.exists():
        raise FileNotFoundError(f"CSV file not found: {path}")
    
    if path.stat().st_size == 0:
        raise ValueError(f"CSV file is empty: {path}")
    
    enc = encoding or detect_encoding(path)
    dialect = detect_dialect(path, enc)

    if delimiter:
        dialect.delimiter = delimiter
        logging.info(f"Using forced delimiter: '{delimiter}'")

    if compute_md5:
        file_md5 = md5sum(path)
    else:
        logging.info("Skipping MD5 hash calculation as requested")
        file_md5 = None

    # Prepare text stream
    try:
        with path.open("r", encoding=enc, errors="replace", newline="") as f:
            reader = csv.reader(f, dialect)

            # Header handling
            if header:
                try:
                    columns = next(reader)
                    logging.info(f"Found {len(columns)} columns in header")
                except StopIteration:
                    raise RuntimeError("Empty CSV; no header row found.")
            else:
                # Peek first row to determine number of columns and then rewind
                try:
                    first_row = next(reader)
                    columns = [f"col_{i+1}" for i in range(len(first_row))]
                    logging.info(f"No header specified, auto-generated {len(columns)} column names")
                    # Rebuild reader: easiest is to reopen file and skip no header
                    f.seek(0)
                    reader = csv.reader(f, dialect)
                except StopIteration:
                    raise RuntimeError("Empty CSV; no data rows found.")

            stats = [ColumnStats(name.strip() or f"col_{i+1}") for i, name in enumerate(columns)]

            data_rows_processed = 0
            
            for row in reader:
                # pad/trim length mismatches defensively
                if len(row) < len(columns):
                    row += [""] * (len(columns) - len(row))
                elif len(row) > len(columns):
                    row = row[: len(columns)]

                for i, val in enumerate(row):
                    stats[i].update(val)

                data_rows_processed += 1
                
                # Progress logging for large files
                if data_rows_processed % 10000 == 0:
                    logging.info(f"Processed {data_rows_processed} rows...")

                if limit_rows and data_rows_processed >= limit_rows:
                    logging.info(f"Reached row limit of {limit_rows}")
                    break

            logging.info(f"Finished profiling. Processed {data_rows_processed} data rows.")
            return columns, stats, {"rows_read": data_rows_processed}, file_md5, dialect
            
    except UnicodeDecodeError as e:
        raise ValueError(f"Error decoding file with encoding '{enc}': {e}")
    except Exception as e:
        raise RuntimeError(f"Error processing CSV file: {e}")


# ------------------------------ RDF emission ------------------------------

def safe_uri_fragment(s: str) -> str:
    # Very simple fragment escaper (letters, digits, _, -). Replace others with '_'.
    import re
    frag = re.sub(r"[^A-Za-z0-9_\-]", "_", s.strip())
    if not frag:
        frag = "unnamed"
    return frag

def build_cdi_rdf(
    columns: List[str],
    stats: List[ColumnStats],
    dataset_pid: str,
    dataset_uri_base: str,
    file_uri: Optional[str],
    dataset_title: Optional[str],
    file_md5: Optional[str],
    out_path: Path,
    ddi_raw: Optional[str] = None,
    ddi_variables: Optional[Dict[str, Dict[str, Any]]] = None,
    ddi_is_xml_literal: bool = False,
):
    """Build and write CDI RDF to file."""
    logging.info("Building CDI RDF graph")
    
    try:
        g = Graph()
        g.bind("cdi", CDI); g.bind("dcterms", DCTERMS); g.bind("prov", PROV); g.bind("skos", SKOS)

        variable_ddi = ddi_variables or {}
        dataset_uri = URIRef(dataset_uri_base.rstrip("/") + "/" + dataset_pid)
        g.add((dataset_uri, RDF.type, CDI.DataSet))
        g.add((dataset_uri, DCTERMS.identifier, Literal(dataset_pid)))
        if dataset_title:
            g.add((dataset_uri, DCTERMS.title, Literal(dataset_title)))

        # Physical layer
        phys = BNode()
        g.add((phys, RDF.type, CDI.PhysicalDataSet))
        g.add((phys, DCTERMS.format, Literal("text/csv")))
        if file_uri:
            g.add((phys, DCTERMS.identifier, URIRef(file_uri)))
        if file_md5:
            g.add((phys, DCTERMS.provenance, Literal(f"md5:{file_md5}")))
        if ddi_raw:
            if ddi_is_xml_literal:
                g.add((phys, DCTERMS.source, Literal(ddi_raw, datatype=RDF.XMLLiteral)))
            else:
                g.add((phys, DCTERMS.source, Literal(ddi_raw)))
        g.add((dataset_uri, LINK["dataset_to_physical"], phys))

        # Logical layer
        logical = BNode()
        g.add((logical, RDF.type, CDI.LogicalDataSet))
        g.add((dataset_uri, LINK["dataset_to_logical"], logical))

        # Variables
        for name, st in zip(columns, stats):
            frag = safe_uri_fragment(name)
            var = URIRef(f"{dataset_uri}#var/{frag}")
            role_node = URIRef(f"{dataset_uri}#role/{frag}")

            g.add((var, RDF.type, CDI.Variable))
            if isinstance(variable_ddi, dict):
                ddi_info = variable_ddi.get(name, {})
            else:
                ddi_info = {}
            label = ddi_info.get("label") if isinstance(ddi_info, dict) else None
            if label:
                g.add((var, SKOS.prefLabel, Literal(label)))
                if label.strip() != name:
                    g.add((var, SKOS.altLabel, Literal(name)))
            else:
                g.add((var, SKOS.prefLabel, Literal(name)))
            g.add((var, DCTERMS.identifier, Literal(name)))
            g.add((var, LINK["variable_to_repr"], st.xsd_datatype()))
            g.add((role_node, RDF.type, CDI.Role))
            g.add((role_node, SKOS.prefLabel, Literal(st.role())))
            g.add((var, LINK["variable_to_role"], role_node))
            g.add((logical, LINK["logical_to_variable"], var))

            if isinstance(ddi_info, dict):
                categories = ddi_info.get("categories") or []
                if categories:
                    cat_parts = []
                    for value, cat_label in categories:
                        if value is None:
                            continue
                        entry = value
                        if cat_label:
                            entry = f"{value}={cat_label}"
                        cat_parts.append(entry)
                    if cat_parts:
                        g.add((var, SKOS.note, Literal("DDI categories: " + "; ".join(cat_parts))))

                stats_map_obj = ddi_info.get("statistics")
                if isinstance(stats_map_obj, dict) and stats_map_obj:
                    stats_parts = [f"{key}={value}" for key, value in sorted(stats_map_obj.items())]
                    if stats_parts:
                        g.add((var, SKOS.note, Literal("DDI stats: " + "; ".join(stats_parts))))

        # Simple provenance
        step = BNode()
        g.add((step, RDF.type, CDI.ProcessStep))
        g.add((step, DCTERMS.description, Literal("Generated CDI from CSV via streaming profiler")))
        g.add((dataset_uri, PROV.wasGeneratedBy, step))

        target_path = str(out_path)
        logging.info(f"Writing RDF output to {target_path}")
        rdf_content = g.serialize(format="turtle")
        if target_path == "-":
            sys.stdout.write(rdf_content)
            if not rdf_content.endswith("\n"):
                sys.stdout.write("\n")
            sys.stdout.flush()
        else:
            Path(target_path).write_text(rdf_content, encoding="utf-8")
        logging.info("RDF output written successfully")
        
    except Exception as e:
        raise RuntimeError(f"Error building or writing RDF: {e}")


# ------------------------------ CLI ------------------------------

def run_xconvert(data_file: Path, syntax_file: Path, work_dir: Path) -> Optional[Path]:
    """
    Run xconvert to generate DDI XML from a syntax file.
    
    Args:
        data_file: Path to the data file (e.g., .dat, .txt)
        syntax_file: Path to the syntax file (.sps, .sas, .do, .dct)
        work_dir: Working directory for temporary files
    
    Returns:
        Path to generated DDI XML file, or None if xconvert fails
    """
    import subprocess
    import tempfile
    
    # Determine xconvert flag based on syntax file extension
    ext = syntax_file.suffix.lower()
    xconvert_flag_map = {
        ".sps": "-sps",  # SPSS
        ".sas": "-sas",  # SAS
        ".do": "-do",    # Stata do file
        ".dct": "-dct",  # Stata dictionary
    }
    
    if ext not in xconvert_flag_map:
        logging.warning(f"Unsupported syntax file type: {ext}")
        return None
    
    flag = xconvert_flag_map[ext]
    
    # Create output DDI file
    ddi_output = work_dir / f"xconvert_{syntax_file.stem}.xml"
    
    try:
        # Run xconvert: xconvert -sps <syntax_file> -ddi <output_file> <data_file>
        cmd = ["xconvert", flag, str(syntax_file), "-ddi", str(ddi_output), str(data_file)]
        logging.info(f"Running xconvert: {' '.join(cmd)}")
        
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=300,  # 5 minute timeout
            cwd=str(work_dir)
        )
        
        if result.returncode != 0:
            logging.error(f"xconvert failed with exit code {result.returncode}")
            if result.stderr:
                logging.error(f"xconvert stderr: {result.stderr}")
            return None
        
        if not ddi_output.exists():
            logging.error(f"xconvert did not create expected output file: {ddi_output}")
            return None
        
        logging.info(f"xconvert succeeded, generated DDI at {ddi_output}")
        return ddi_output
        
    except subprocess.TimeoutExpired:
        logging.error("xconvert timed out after 5 minutes")
        return None
    except FileNotFoundError:
        logging.error("xconvert executable not found in PATH")
        return None
    except Exception as exc:
        logging.error(f"xconvert execution failed: {exc}")
        return None


def detect_and_run_xconvert(csv_path: Path, work_dir: Path) -> Optional[Path]:
    """
    Detect if there's a matching syntax file for the data file and run xconvert.
    
    Args:
        csv_path: Path to the data file
        work_dir: Working directory
    
    Returns:
        Path to generated DDI XML, or None if no syntax file found or xconvert fails
    """
    # Check for syntax files in the same directory
    parent = csv_path.parent
    stem = csv_path.stem
    
    # Try different syntax file extensions
    syntax_extensions = [".sps", ".SPS", ".sas", ".SAS", ".do", ".DO", ".dct", ".DCT"]
    
    for ext in syntax_extensions:
        syntax_file = parent / f"{stem}{ext}"
        if syntax_file.exists():
            logging.info(f"Found syntax file: {syntax_file}")
            ddi_path = run_xconvert(csv_path, syntax_file, work_dir)
            if ddi_path:
                return ddi_path
    
    return None


# ------------------------------ CLI ------------------------------

def parse_args():
    p = argparse.ArgumentParser(description="Stream a CSV and emit DDI-CDI RDF (Turtle).")
    p.add_argument("--csv", required=True, type=Path, help="Path to CSV file")
    p.add_argument("--dataset-pid", required=True, help="Dataset PID/DOI (e.g., doi:10.xxxx/xxxx)")
    p.add_argument("--dataset-uri-base", required=True, help="Base URI for dataset landing pages")
    p.add_argument("--file-uri", help="Public URI for this data file (if any)")
    p.add_argument("--dataset-title", help="Dataset title (optional)")
    p.add_argument("--dataset-metadata-file", type=Path, help="Optional path to Dataverse dataset metadata JSON")
    p.add_argument("--output", "-o", type=Path, default=Path("dataset.cdi.ttl"), help="Output TTL path")
    p.add_argument("--delimiter", help="Force CSV delimiter (otherwise sniffed)")
    p.add_argument("--encoding", help="Force encoding (otherwise detected)")
    p.add_argument("--no-header", action="store_true", help="Treat the CSV as headerless")
    p.add_argument("--limit-rows", type=int, help="Optional cap for rows to process (for quick trial runs)")
    p.add_argument("--verbose", "-v", action="store_true", help="Enable verbose logging")
    p.add_argument("--skip-md5", action="store_true", help="Skip MD5 checksum calculation for faster runs")
    p.add_argument("--summary-json", type=Path, help="Optional path to write column summary as JSON")
    p.add_argument("--quiet", action="store_true", help="Suppress console summary output")
    p.add_argument("--ddi-file", type=Path, help="Optional path to a Dataverse DDI fragment for this file")
    return p.parse_args()


def main():
    args = parse_args()
    
    # Load metadata from file if provided, otherwise try stdin (legacy)
    metadata_payload: Optional[Dict[str, Any]] = None
    if args.dataset_metadata_file:
        metadata_payload = load_metadata_from_file(args.dataset_metadata_file)
    else:
        metadata_payload = read_metadata_from_stdin()
    
    # Extract title and file URI from metadata if not provided as arguments
    if metadata_payload:
        if not args.dataset_title:
            inferred_title = extract_dataset_title(metadata_payload)
            if inferred_title:
                args.dataset_title = inferred_title
        
        if not args.file_uri:
            # Extract base URL from dataset_uri_base
            base_url = args.dataset_uri_base.replace("/dataset", "")
            inferred_uri = extract_file_uri(metadata_payload, args.csv.name, base_url)
            if inferred_uri:
                args.file_uri = inferred_uri

    # Set up logging
    setup_logging(args.verbose, args.quiet)

    ddi_raw: Optional[str] = None
    ddi_variables: Dict[str, Dict[str, Any]] = {}
    ddi_is_xml_literal = False
    
    # Try xconvert if no DDI file provided
    if not args.ddi_file:
        logging.info("No DDI file provided, checking for xconvert-compatible syntax files")
        work_path = Path(args.csv).parent
        xconvert_ddi = detect_and_run_xconvert(args.csv, work_path)
        if xconvert_ddi:
            args.ddi_file = xconvert_ddi
            logging.info(f"Using xconvert-generated DDI: {xconvert_ddi}")
    
    if args.ddi_file:
        raw_ddi, parsed_variables, is_xml_literal = load_ddi_metadata(args.ddi_file)
        if raw_ddi:
            ddi_raw = raw_ddi
            ddi_variables = parsed_variables
            ddi_is_xml_literal = is_xml_literal
            logging.info("Loaded DDI fragment from %s", args.ddi_file)
            if not parsed_variables:
                logging.info("DDI fragment contained no variable-level metadata")
        else:
            logging.warning("DDI metadata unavailable for %s", args.ddi_file)

    try:
        logging.info("Starting CSV to CDI conversion")
        logging.info(f"Input file: {args.csv}")
        logging.info(f"Output file: {args.output}")
        
        cols, stats, info, md5, dialect = stream_profile_csv(
            args.csv,
            encoding=args.encoding,
            delimiter=args.delimiter,
            header=not args.no_header,
            limit_rows=args.limit_rows,
            compute_md5=not args.skip_md5,
        )

        build_cdi_rdf(
            columns=cols,
            stats=stats,
            dataset_pid=args.dataset_pid,
            dataset_uri_base=args.dataset_uri_base,
            file_uri=args.file_uri,
            dataset_title=args.dataset_title,
            file_md5=md5,
            out_path=args.output,
            ddi_raw=ddi_raw,
            ddi_variables=ddi_variables,
            ddi_is_xml_literal=ddi_is_xml_literal,
        )

        if not args.quiet:
            print(f"[OK] Wrote CDI TTL: {args.output}")
            print(f"  rows_profiled={info['rows_read']}, columns={len(cols)}")

        # Print column summary
        if not args.quiet:
            print("\nColumn Analysis:")
        column_summaries: List[Dict[str, object]] = []
        for name, st in zip(cols, stats):
            datatype_name = st.xsd_datatype().split('#')[-1] if hasattr(st.xsd_datatype(),'split') else str(st.xsd_datatype())
            distinct = st.approx_distinct()
            non_missing = st.n_non_missing
            role = st.role()
            ddi_meta = ddi_variables.get(name, {}) if isinstance(ddi_variables, dict) else {}
            ddi_label = ddi_meta.get("label") if isinstance(ddi_meta, dict) else None
            extra_label = ""
            if ddi_label and ddi_label != name:
                extra_label = f" | ddi_label={ddi_label}"
            if not args.quiet:
                print(
                    f"  - {name:15} | type={datatype_name:10} | role={role:10} | distinct={distinct:6} | non-missing={non_missing:6}{extra_label}"
                )
            summary_entry: Dict[str, Any] = {
                "name": name,
                "datatype": datatype_name,
                "role": role,
                "approx_distinct": distinct,
                "non_missing": non_missing,
            }
            if ddi_label:
                summary_entry["ddi_label"] = ddi_label
            if isinstance(ddi_meta, dict):
                categories = [
                    {"value": value, "label": label}
                    for value, label in (ddi_meta.get("categories") or [])
                    if value is not None
                ]
                if categories:
                    summary_entry["ddi_categories"] = categories
                stats_map_obj = ddi_meta.get("statistics")
                if isinstance(stats_map_obj, dict) and stats_map_obj:
                    summary_entry["ddi_statistics"] = stats_map_obj
            column_summaries.append(summary_entry)

        if args.summary_json:
            summary_path = args.summary_json
            summary_parent = summary_path.parent
            if summary_parent != Path("."):
                summary_parent.mkdir(parents=True, exist_ok=True)
            summary_payload = {
                "dataset_pid": args.dataset_pid,
                "rows_profiled": info["rows_read"],
                "columns": column_summaries,
            }
            summary_path.write_text(json.dumps(summary_payload, indent=2), encoding="utf-8")
            logging.info(f"Wrote column summary JSON to {summary_path}")
            
        logging.info("Conversion completed successfully")
        
    except Exception as e:
        logging.error(f"Error during conversion: {e}")
        print(f"[ERROR] {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()