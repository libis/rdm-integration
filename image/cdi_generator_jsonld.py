#!/usr/bin/env python3
# ----------------------------- requirements.txt -----------------------------
# rdflib==7.0.0
# chardet==5.2.0
# datasketch==1.5.9
# python-dateutil==2.9.0.post0
# ---------------------------------------------------------------------------

"""
CSV/TSV -> DDI-CDI JSON-LD generation utilities.

- Streams large tabular files row-by-row; never loads the whole file.
- Infers per-column XSD datatype and a role (identifier/dimension/measure/attribute).
- Uses HyperLogLog (datasketch) to approximate distinct counts with tiny memory.
- Emits DDI-CDI 1.0 compliant JSON-LD with official context.
- Supports both single-file conversion and dataset manifests that describe many files at once.

USAGE
------

Dataset manifest (recommended):

    python cdi_generator_jsonld.py \\
        --manifest /tmp/manifest.json \\
        --output /tmp/dataset.cdi.jsonld \\
        --quiet

Single file (legacy mode retained for compatibility):

    python cdi_generator_jsonld.py \\
        --csv /data/big.csv \\
        --dataset-pid "doi:10.70122/FK2/EXAMPLE" \\
        --dataset-uri-base "https://rdr.kuleuven.be/dataset" \\
        --file-uri "https://rdr.kuleuven.be/api/access/datafile/123456" \\
        --dataset-title "Example dataset" \\
        --output dataset.cdi.jsonld

Notes
-----
- Header auto-detects by default; use --no-header to force synthetic column names.
- Encoding is detected on a sample via chardet; override with --encoding if needed.
- Delimiter is sniffed unless provided with --delimiter.
- Output is JSON-LD with official DDI-CDI 1.0 context.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import logging
import re
import sys
from pathlib import Path
from typing import Any, List, Optional, Dict, Tuple, Union
import xml.etree.ElementTree as ET
import chardet
from datasketch import HyperLogLog
from dateutil import parser as dateparser


# ---- Official DDI-CDI 1.0 JSON-LD Context URL ----
DDI_CDI_CONTEXT = "https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld"

# ---- DDI Controlled Vocabulary Data Types ----
DDI_DATATYPE_CV = "http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#"

# Mapping from XSD types to DDI CV data types
XSD_TO_DDI_DATATYPE = {
    "integer": f"{DDI_DATATYPE_CV}Integer",
    "decimal": f"{DDI_DATATYPE_CV}Double",
    "double": f"{DDI_DATATYPE_CV}Double",
    "boolean": f"{DDI_DATATYPE_CV}Boolean",
    "date": f"{DDI_DATATYPE_CV}Date",
    "dateTime": f"{DDI_DATATYPE_CV}DateTime",
    "string": f"{DDI_DATATYPE_CV}String",
}

# Mapping from inferred roles to DDI-CDI component types
ROLE_TO_COMPONENT_TYPE = {
    "identifier": "IdentifierComponent",
    "measure": "MeasureComponent",
    "dimension": "DimensionComponent",
    "attribute": "AttributeComponent",
}


# -------------------------- Streaming type inference --------------------------

MISSING = {"", "na", "n/a", "null", "none", "nan", "NA", "N/A", "NULL", "None", "NaN"}

def is_int(s: str) -> bool:
    """Check if string represents an integer."""
    try:
        int(s)
        return True
    except (ValueError, TypeError):
        return False

def is_float(s: str) -> bool:
    """Check if string represents a float (but not an integer)."""
    try:
        float(s)
        return not is_int(s)
    except (ValueError, TypeError):
        return False

def is_bool(s: str) -> bool:
    """Check if string represents a boolean value."""
    return s.lower() in {"true", "false", "t", "f", "0", "1", "yes", "no", "y", "n"}

def is_datetime(s: str) -> bool:
    """Check if string represents a datetime value."""
    try:
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
        self.hll = HyperLogLog(p=12)
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

        self.hll.update(s.encode("utf-8", "ignore"))

        if self.could_be_int and not is_int(s):
            self.could_be_int = False

        if self.could_be_float and not (is_float(s) or is_int(s)):
            self.could_be_float = False

        if self.could_be_bool and not is_bool(s):
            self.could_be_bool = False

        if self.could_be_datetime and not is_datetime(s):
            self.could_be_datetime = False

    def xsd_datatype_name(self) -> str:
        """Return XSD datatype name (without namespace)."""
        if self.could_be_int and self.n_non_missing > 0:
            return "integer"
        if self.could_be_float and self.n_non_missing > 0:
            return "decimal"
        if self.could_be_bool and self.n_non_missing > 0:
            return "boolean"
        if self.could_be_datetime and self.n_non_missing > 0:
            return "dateTime"
        return "string"

    def ddi_datatype_uri(self) -> str:
        """Return DDI CV datatype URI."""
        return XSD_TO_DDI_DATATYPE.get(self.xsd_datatype_name(), f"{DDI_DATATYPE_CV}String")

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

        dt = self.xsd_datatype_name()
        if dt in ("integer", "decimal") and uniq_ratio < 0.95:
            return "measure"

        if dt == "boolean" or distinct <= min(50, int(0.1 * self.n_non_missing)):
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
        class _D(csv.Dialect):
            delimiter = ","
            quotechar = '"'
            doublequote = True
            skipinitialspace = False
            lineterminator = "\n"
            quoting = csv.QUOTE_MINIMAL
        return _D()


def detect_header_mode(
    path: Path,
    encoding: str,
    dialect: csv.Dialect,
    sample_bytes: int = 256 * 1024,
    typed_threshold: float = 0.75,
) -> bool:
    """Heuristically determine whether the file has a header row."""

    sample_text = ""
    try:
        with path.open("r", encoding=encoding, errors="replace", newline="") as f:
            sample_text = f.read(sample_bytes)
    except Exception as exc:
        logging.debug("Failed to read sample for header detection: %s", exc)

    sniffed_header = True
    if sample_text.strip():
        try:
            sniffer = csv.Sniffer()
            sniffed_header = sniffer.has_header(sample_text)
        except Exception as exc:
            logging.debug("csv.Sniffer header detection failed: %s", exc)

    first_row: List[str] = []
    try:
        with path.open("r", encoding=encoding, errors="replace", newline="") as f:
            reader = csv.reader(f, dialect)
            first_row = next(reader)
    except StopIteration:
        logging.debug("File %s appears to be empty during header detection", path)
        return False
    except Exception as exc:
        logging.debug("Failed to analyse first rows for header detection: %s", exc)
        return sniffed_header

    typed_cells = 0
    total_cells = 0
    for cell in first_row:
        value = cell.strip()
        if not value:
            continue
        total_cells += 1
        if is_int(value) or is_float(value) or is_bool(value) or is_datetime(value):
            typed_cells += 1

    if total_cells:
        ratio = typed_cells / total_cells
    else:
        ratio = 0.0

    looks_like_data_row = ratio >= typed_threshold

    if looks_like_data_row:
        logging.info(
            "Header auto-detect: first row of %s resembles data (typed_ratio=%.2f); treating as no header",
            path,
            ratio,
        )
        return False

    return sniffed_header

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


def safe_fragment(s: str) -> str:
    """Convert string to safe URI fragment identifier."""
    frag = re.sub(r"[^A-Za-z0-9_\-]", "_", s.strip())
    if not frag:
        frag = "unnamed"
    return frag


def stream_profile_csv(
    path: Path,
    encoding: Optional[str] = None,
    delimiter: Optional[str] = None,
    header: Union[bool, str] = "auto",
    limit_rows: Optional[int] = None,
    compute_md5: bool = True,
) -> Tuple[List[str], List[ColumnStats], Dict[str, int], Optional[str], csv.Dialect]:
    """
    Stream the CSV once and build column stats.
    Returns: (column_names, stats[], row_count_info, file_md5, dialect)
    """
    logging.info(f"Starting to profile CSV: {path}")
    
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

    try:
        with path.open("r", encoding=enc, errors="replace", newline="") as f:
            reader = csv.reader(f, dialect)

            header_decision: bool
            auto_detect = False
            if isinstance(header, str):
                header_key = header.lower()
                if header_key == "auto":
                    auto_detect = True
                elif header_key in {"present", "true", "yes"}:
                    header_decision = True
                elif header_key in {"absent", "false", "no"}:
                    header_decision = False
                else:
                    raise ValueError(f"Invalid header mode: {header}")
            else:
                header_decision = bool(header)

            if auto_detect:
                header_decision = detect_header_mode(path, enc, dialect)
                logging.info("Header auto-detection result for %s: %s", path, header_decision)

            if header_decision:
                try:
                    columns = next(reader)
                    logging.info(f"Found {len(columns)} columns in header")
                except StopIteration:
                    raise RuntimeError("Empty CSV; no header row found.")
            else:
                try:
                    first_row = next(reader)
                    columns = [f"col_{i+1}" for i in range(len(first_row))]
                    logging.info(f"No header specified, auto-generated {len(columns)} column names")
                    f.seek(0)
                    reader = csv.reader(f, dialect)
                except StopIteration:
                    raise RuntimeError("Empty CSV; no data rows found.")

            stats = [ColumnStats(name.strip() or f"col_{i+1}") for i, name in enumerate(columns)]

            data_rows_processed = 0
            
            for row in reader:
                if len(row) < len(columns):
                    row += [""] * (len(columns) - len(row))
                elif len(row) > len(columns):
                    row = row[: len(columns)]

                for i, val in enumerate(row):
                    stats[i].update(val)

                data_rows_processed += 1
                
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


# ------------------------------ DDI Metadata Loading ------------------------------

def _strip_ddi_tag(tag: str) -> str:
    return tag.split('}', 1)[1] if '}' in tag else tag


def load_ddi_metadata(ddi_path: Path) -> Tuple[Optional[str], Dict[str, Dict[str, Any]], bool]:
    """Read and parse a DDI fragment, returning sanitized XML and per-variable metadata."""
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


# ------------------------------ Dataverse Metadata Extraction ------------------------------

def load_metadata_from_file(path: Path) -> Optional[Dict[str, Any]]:
    """Load dataset metadata from a JSON file."""
    if not path.exists():
        return None
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        logging.warning("Failed to parse metadata from %s: %s", path, exc)
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


def extract_dataset_description(metadata: Dict[str, Any]) -> Optional[str]:
    """Extract dataset description from Dataverse-style metadata."""
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
        type_name = field.get("typeName")
        if type_name not in ("dsDescription", "description"):
            continue
        value = field.get("value")
        if isinstance(value, str):
            return value
        if isinstance(value, list) and value:
            first = value[0]
            if isinstance(first, str):
                return first
            if isinstance(first, dict):
                desc_value = first.get("dsDescriptionValue") or first.get("value")
                if isinstance(desc_value, dict):
                    return desc_value.get("value")
                if isinstance(desc_value, str):
                    return desc_value
    return None


def _get_citation_fields(metadata: Dict[str, Any]) -> Optional[List[Dict[str, Any]]]:
    """Get citation fields list from Dataverse metadata structure."""
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
    return fields if isinstance(fields, list) else None


def _extract_field_value(fields: List[Dict[str, Any]], type_name: str) -> Optional[str]:
    """Extract a simple string value from Dataverse citation fields."""
    for field in fields:
        if not isinstance(field, dict):
            continue
        if field.get("typeName") != type_name:
            continue
        value = field.get("value")
        if isinstance(value, str):
            return value
        if isinstance(value, list) and value:
            first = value[0]
            if isinstance(first, str):
                return first
    return None


def _extract_compound_list(
    fields: List[Dict[str, Any]], type_name: str, sub_field: str
) -> List[str]:
    """Extract list of sub-field values from a compound Dataverse field."""
    results: List[str] = []
    for field in fields:
        if not isinstance(field, dict):
            continue
        if field.get("typeName") != type_name:
            continue
        value = field.get("value")
        if not isinstance(value, list):
            continue
        for item in value:
            if not isinstance(item, dict):
                continue
            sub_val = item.get(sub_field)
            if isinstance(sub_val, dict):
                sub_val = sub_val.get("value")
            if isinstance(sub_val, str) and sub_val.strip():
                results.append(sub_val.strip())
    return results


def extract_rich_metadata(metadata: Dict[str, Any]) -> Dict[str, Any]:
    """Extract comprehensive dataset metadata from Dataverse JSON.
    
    Returns a dict with keys:
        - title: str
        - subtitle: str (alternative title)
        - description: str (abstract/summary)
        - authors: List[Dict] (name, affiliation, identifier)
        - contributors: List[Dict]
        - publisher: str
        - publication_date: str (ISO format)
        - keywords: List[str]
        - subjects: List[str]
        - language: str
        - license_name: str
        - license_uri: str
        - related_publications: List[str]
        - grant_numbers: List[str]
        - depositor: str
        - date_of_deposit: str
        - production_date: str
        - distribution_date: str
    """
    result: Dict[str, Any] = {}
    
    fields = _get_citation_fields(metadata)
    if not fields:
        return result
    
    # Title
    result["title"] = _extract_field_value(fields, "title")
    
    # Subtitle (alternative title)
    result["subtitle"] = _extract_field_value(fields, "subtitle")
    
    # Alternative title
    alt_titles = _extract_compound_list(fields, "alternativeTitle", "alternativeTitleValue")
    if alt_titles:
        result["alternative_title"] = alt_titles[0]
    
    # Authors with affiliation and identifier
    authors: List[Dict[str, Any]] = []
    for field in fields:
        if not isinstance(field, dict):
            continue
        if field.get("typeName") != "author":
            continue
        value = field.get("value")
        if not isinstance(value, list):
            continue
        for item in value:
            if not isinstance(item, dict):
                continue
            author: Dict[str, Any] = {}
            name_val = item.get("authorName")
            if isinstance(name_val, dict):
                name_val = name_val.get("value")
            if name_val:
                author["name"] = name_val
            
            affil_val = item.get("authorAffiliation")
            if isinstance(affil_val, dict):
                affil_val = affil_val.get("value")
            if affil_val:
                author["affiliation"] = affil_val
            
            ident_scheme = item.get("authorIdentifierScheme")
            if isinstance(ident_scheme, dict):
                ident_scheme = ident_scheme.get("value")
            ident_val = item.get("authorIdentifier")
            if isinstance(ident_val, dict):
                ident_val = ident_val.get("value")
            if ident_val:
                author["identifier"] = ident_val
                if ident_scheme:
                    author["identifier_scheme"] = ident_scheme
            
            if author:
                authors.append(author)
    if authors:
        result["authors"] = authors
    
    # Contributors (similar structure)
    contributors: List[Dict[str, Any]] = []
    for field in fields:
        if not isinstance(field, dict):
            continue
        if field.get("typeName") != "contributor":
            continue
        value = field.get("value")
        if not isinstance(value, list):
            continue
        for item in value:
            if not isinstance(item, dict):
                continue
            contrib: Dict[str, Any] = {}
            name_val = item.get("contributorName")
            if isinstance(name_val, dict):
                name_val = name_val.get("value")
            if name_val:
                contrib["name"] = name_val
            
            type_val = item.get("contributorType")
            if isinstance(type_val, dict):
                type_val = type_val.get("value")
            if type_val:
                contrib["type"] = type_val
            
            if contrib:
                contributors.append(contrib)
    if contributors:
        result["contributors"] = contributors
    
    # Publisher
    result["publisher"] = _extract_field_value(fields, "distributorName")
    if not result.get("publisher"):
        result["publisher"] = _extract_field_value(fields, "publisher")
    
    # Contact (as backup publisher info)
    contacts = _extract_compound_list(fields, "datasetContact", "datasetContactName")
    if contacts and not result.get("publisher"):
        result["publisher"] = contacts[0]
    
    # Publication/Distribution date
    result["publication_date"] = _extract_field_value(fields, "distributionDate")
    if not result.get("publication_date"):
        result["publication_date"] = _extract_field_value(fields, "dateOfDeposit")
    
    # Production date
    result["production_date"] = _extract_field_value(fields, "productionDate")
    
    # Keywords
    keywords = _extract_compound_list(fields, "keyword", "keywordValue")
    if keywords:
        result["keywords"] = keywords
    
    # Subjects
    subjects: List[str] = []
    for field in fields:
        if not isinstance(field, dict):
            continue
        if field.get("typeName") != "subject":
            continue
        value = field.get("value")
        if isinstance(value, str):
            subjects.append(value)
        elif isinstance(value, list):
            for v in value:
                if isinstance(v, str):
                    subjects.append(v)
    if subjects:
        result["subjects"] = subjects
    
    # Language
    for field in fields:
        if not isinstance(field, dict):
            continue
        if field.get("typeName") != "language":
            continue
        value = field.get("value")
        if isinstance(value, str):
            result["language"] = value
        elif isinstance(value, list) and value:
            result["language"] = value[0] if isinstance(value[0], str) else None
    
    # License
    dataset_version = metadata.get("datasetVersion") or metadata.get("latestVersion")
    if isinstance(dataset_version, dict):
        license_info = dataset_version.get("license")
        if isinstance(license_info, dict):
            result["license_name"] = license_info.get("name")
            result["license_uri"] = license_info.get("uri")
        terms = dataset_version.get("termsOfUse")
        if isinstance(terms, str):
            result["terms_of_use"] = terms
    
    # Related publications
    related_pubs = _extract_compound_list(fields, "publication", "publicationCitation")
    if related_pubs:
        result["related_publications"] = related_pubs
    
    # Related materials (URLs)
    related_urls = _extract_compound_list(fields, "publication", "publicationURL")
    if related_urls:
        result["related_urls"] = related_urls
    
    # Grant/Funding numbers
    grants = _extract_compound_list(fields, "grantNumber", "grantNumberValue")
    if grants:
        result["grant_numbers"] = grants
    
    # Funding agencies
    funders = _extract_compound_list(fields, "grantNumber", "grantNumberAgency")
    if funders:
        result["funding_agencies"] = funders
    
    # Depositor
    result["depositor"] = _extract_field_value(fields, "depositor")
    
    # Date of deposit
    result["date_of_deposit"] = _extract_field_value(fields, "dateOfDeposit")
    
    # Time period covered
    time_period_start = _extract_compound_list(fields, "timePeriodCovered", "timePeriodCoveredStart")
    time_period_end = _extract_compound_list(fields, "timePeriodCovered", "timePeriodCoveredEnd")
    if time_period_start:
        result["time_period_start"] = time_period_start[0]
    if time_period_end:
        result["time_period_end"] = time_period_end[0]
    
    # Geographic coverage
    geo_coverage = _extract_compound_list(fields, "geographicCoverage", "country")
    if geo_coverage:
        result["geographic_coverage"] = geo_coverage
    
    return {k: v for k, v in result.items() if v}  # Remove empty values


# ------------------------------ JSON-LD Graph Building ------------------------------

def _build_catalog_details(
    rich_metadata: Dict[str, Any],
    dataset_pid: Optional[str] = None,
) -> Dict[str, Any]:
    """Build a DDI-CDI CatalogDetails node from rich metadata.
    
    This follows the SHACL shape for CatalogDetails which supports:
    - title, subTitle, alternativeTitle
    - summary (abstract)
    - creator, contributor, publisher (as AgentInRole)
    - date (CombinedDate)
    - access (AccessInformation with license)
    - languageOfObject
    - provenance (with funding info)
    - relatedResource
    - identifier (InternationalIdentifier)
    """
    catalog: Dict[str, Any] = {
        "@id": "#catalogDetails",
        "@type": "CatalogDetails",
    }
    
    # Title (InternationalString)
    if rich_metadata.get("title"):
        catalog["title"] = {
            "@type": "InternationalString",
            "languageSpecificString": {
                "@type": "LanguageString",
                "content": rich_metadata["title"],
            }
        }
    
    # Subtitle
    if rich_metadata.get("subtitle"):
        catalog["subTitle"] = {
            "@type": "InternationalString",
            "languageSpecificString": {
                "@type": "LanguageString",
                "content": rich_metadata["subtitle"],
            }
        }
    
    # Alternative title
    if rich_metadata.get("alternative_title"):
        catalog["alternativeTitle"] = {
            "@type": "InternationalString",
            "languageSpecificString": {
                "@type": "LanguageString",
                "content": rich_metadata["alternative_title"],
            }
        }
    
    # Summary (description/abstract)
    if rich_metadata.get("description"):
        catalog["summary"] = {
            "@type": "InternationalString",
            "languageSpecificString": {
                "@type": "LanguageString",
                "content": rich_metadata["description"],
            }
        }
    
    # Creators (authors as AgentInRole)
    if rich_metadata.get("authors"):
        creators: List[Dict[str, Any]] = []
        for i, author in enumerate(rich_metadata["authors"]):
            agent_id = f"#author_{i}"
            agent_in_role: Dict[str, Any] = {
                "@type": "AgentInRole",
                "agent": {
                    "@id": agent_id,
                    "@type": "Individual",
                }
            }
            
            # Build individual name (BibliographicName with affiliation)
            if author.get("name"):
                name_node: Dict[str, Any] = {
                    "@type": "BibliographicName",
                    "languageSpecificString": {
                        "@type": "LanguageString",
                        "content": author["name"],
                    }
                }
                if author.get("affiliation"):
                    name_node["affiliation"] = author["affiliation"]
                agent_in_role["agent"]["individualName"] = name_node
            
            # Add identifier (e.g., ORCID)
            if author.get("identifier"):
                ident_node: Dict[str, Any] = {
                    "@type": "Identifier",
                }
                ident_value = author["identifier"]
                scheme = author.get("identifier_scheme", "")
                if scheme.upper() == "ORCID" and not ident_value.startswith("http"):
                    ident_node["uri"] = f"https://orcid.org/{ident_value}"
                else:
                    ident_node["uri"] = ident_value
                agent_in_role["agent"]["identifier"] = ident_node
            
            creators.append(agent_in_role)
        
        if creators:
            catalog["creator"] = creators
    
    # Contributors
    if rich_metadata.get("contributors"):
        contributors: List[Dict[str, Any]] = []
        for i, contrib in enumerate(rich_metadata["contributors"]):
            agent_id = f"#contributor_{i}"
            agent_in_role: Dict[str, Any] = {
                "@type": "AgentInRole",
                "agent": {
                    "@id": agent_id,
                    "@type": "Individual",
                }
            }
            if contrib.get("name"):
                agent_in_role["agent"]["individualName"] = {
                    "@type": "BibliographicName",
                    "languageSpecificString": {
                        "@type": "LanguageString",
                        "content": contrib["name"],
                    }
                }
            if contrib.get("type"):
                agent_in_role["role"] = contrib["type"]
            
            contributors.append(agent_in_role)
        
        if contributors:
            catalog["contributor"] = contributors
    
    # Publisher
    if rich_metadata.get("publisher"):
        catalog["publisher"] = {
            "@type": "AgentInRole",
            "agent": {
                "@id": "#publisher",
                "@type": "Organization",
                "organizationName": {
                    "@type": "OrganizationName",
                    "name": {
                        "@type": "LanguageString",
                        "content": rich_metadata["publisher"],
                    }
                }
            }
        }
    
    # Date (publication/distribution date)
    if rich_metadata.get("publication_date"):
        catalog["date"] = {
            "@type": "CombinedDate",
            "isoDate": rich_metadata["publication_date"],
        }
    
    # Language
    if rich_metadata.get("language"):
        catalog["languageOfObject"] = rich_metadata["language"]
    
    # Access information (license)
    if rich_metadata.get("license_name") or rich_metadata.get("license_uri"):
        access: Dict[str, Any] = {
            "@type": "AccessInformation",
        }
        if rich_metadata.get("license_name") or rich_metadata.get("license_uri"):
            license_info: Dict[str, Any] = {
                "@type": "LicenseInformation",
            }
            if rich_metadata.get("license_name"):
                license_info["name"] = {
                    "@type": "InternationalString",
                    "languageSpecificString": {
                        "@type": "LanguageString",
                        "content": rich_metadata["license_name"],
                    }
                }
            if rich_metadata.get("license_uri"):
                license_info["uri"] = rich_metadata["license_uri"]
            access["license"] = license_info
        
        if rich_metadata.get("terms_of_use"):
            access["rights"] = {
                "@type": "InternationalString",
                "languageSpecificString": {
                    "@type": "LanguageString",
                    "content": rich_metadata["terms_of_use"],
                }
            }
        
        catalog["access"] = access
    
    # Provenance (funding information)
    if rich_metadata.get("grant_numbers") or rich_metadata.get("funding_agencies"):
        provenance: Dict[str, Any] = {
            "@type": "ProvenanceInformation",
        }
        
        funding_list: List[Dict[str, Any]] = []
        agencies = rich_metadata.get("funding_agencies", [])
        grants = rich_metadata.get("grant_numbers", [])
        
        # Pair agencies with grants
        for i, grant in enumerate(grants):
            funding: Dict[str, Any] = {
                "@type": "FundingInformation",
                "grantIdentifier": grant,
            }
            if i < len(agencies) and agencies[i]:
                funding["funder"] = agencies[i]
            funding_list.append(funding)
        
        # Add any remaining agencies without grants
        for agency in agencies[len(grants):]:
            if agency:
                funding_list.append({
                    "@type": "FundingInformation",
                    "funder": agency,
                })
        
        if funding_list:
            provenance["funding"] = funding_list
        
        # Deposit date
        if rich_metadata.get("date_of_deposit"):
            provenance["recordCreationDate"] = rich_metadata["date_of_deposit"]
        
        catalog["provenance"] = provenance
    
    # Identifier (PID)
    if dataset_pid:
        catalog["identifier"] = {
            "@type": "InternationalIdentifier",
            "identifierContent": dataset_pid,
            "isURI": dataset_pid.startswith("http") or dataset_pid.startswith("doi:"),
        }
    
    # Related resources (publications)
    if rich_metadata.get("related_publications") or rich_metadata.get("related_urls"):
        related: List[Dict[str, Any]] = []
        
        for pub in rich_metadata.get("related_publications", []):
            related.append({
                "@type": "Reference",
                "description": {
                    "@type": "InternationalString",
                    "languageSpecificString": {
                        "@type": "LanguageString",
                        "content": pub,
                    }
                }
            })
        
        for url in rich_metadata.get("related_urls", []):
            related.append({
                "@type": "Reference",
                "uri": url,
            })
        
        if related:
            catalog["relatedResource"] = related
    
    # Type of resource (subjects/keywords as ControlledVocabularyEntry)
    if rich_metadata.get("subjects"):
        catalog["typeOfResource"] = {
            "@type": "ControlledVocabularyEntry",
            "entryValue": "; ".join(rich_metadata["subjects"]),
        }
    
    return catalog


def build_jsonld_graph(
    dataset_title: str,
    dataset_description: Optional[str],
    files_data: List[Dict[str, Any]],
    dataset_pid: Optional[str] = None,
    rich_metadata: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    """
    Build a DDI-CDI 1.0 compliant JSON-LD document.
    
    Args:
        dataset_title: Title of the dataset
        dataset_description: Optional description
        files_data: List of file data, each containing columns and stats
        dataset_pid: Optional persistent identifier
        rich_metadata: Optional rich metadata extracted from Dataverse
    
    Returns:
        JSON-LD document as Python dict
    """
    graph: List[Dict[str, Any]] = []
    
    # Create dataset ID from title
    dataset_id = f"#{safe_fragment(dataset_title)}"
    structure_id = "#datastructure"
    logical_record_id = "#logicalRecord"
    physical_layout_id = "#physicalSegmentLayout"
    catalog_details_id = "#catalogDetails"
    
    # Build CatalogDetails from rich metadata if available
    if rich_metadata:
        # Ensure description is in rich_metadata for CatalogDetails
        if dataset_description and not rich_metadata.get("description"):
            rich_metadata["description"] = dataset_description
        catalog_details = _build_catalog_details(rich_metadata, dataset_pid)
        graph.append(catalog_details)
    
    # Collect all components and variables
    all_component_ids: List[str] = []
    all_variable_ids: List[str] = []
    all_value_mappings: List[str] = []
    all_value_mapping_positions: List[str] = []
    primary_key_components: List[str] = []
    
    # Process all variables from all files
    for file_data in files_data:
        columns = file_data.get("columns", [])
        stats = file_data.get("stats", [])
        ddi_variables = file_data.get("ddi_variables", {})
        file_name = file_data.get("file_name", "")
        
        # Create a file prefix for unique IDs across files
        file_prefix = safe_fragment(Path(file_name).stem) if file_name else ""
        
        for col_name, col_stats in zip(columns, stats):
            var_frag = safe_fragment(col_name)
            # Include file prefix to ensure unique IDs across files
            full_frag = f"{file_prefix}_{var_frag}" if file_prefix else var_frag
            var_id = f"#{full_frag}"
            domain_id = f"#{full_frag}_Substantive_Value_Domain"
            component_id = f"#{full_frag}_Component"
            mapping_id = f"#valueMapping_{full_frag}"
            mapping_pos_id = f"#ValueMappingPosition_{full_frag}"
            
            all_variable_ids.append(var_id)
            all_component_ids.append(component_id)
            all_value_mappings.append(mapping_id)
            all_value_mapping_positions.append(mapping_pos_id)
            
            # Get DDI metadata if available
            ddi_info = ddi_variables.get(col_name, {}) if isinstance(ddi_variables, dict) else {}
            label = ddi_info.get("label") if isinstance(ddi_info, dict) else None
            categories = ddi_info.get("categories", []) if isinstance(ddi_info, dict) else []
            statistics = ddi_info.get("statistics", {}) if isinstance(ddi_info, dict) else {}
            
            # Determine role and component type
            role = col_stats.role()
            component_type = ROLE_TO_COMPONENT_TYPE.get(role, "AttributeComponent")
            
            # Track identifier components for primary key
            if role == "identifier":
                primary_key_components.append(component_id)
            
            # Create CodeList if we have categories (value labels)
            codelist_id = None
            if categories:
                codelist_id = f"#{full_frag}_CodeList"
                code_ids = []
                
                for cat_value, cat_label in categories:
                    code_frag = safe_fragment(str(cat_value))
                    code_id = f"#{full_frag}_Code_{code_frag}"
                    code_ids.append(code_id)
                    
                    code_node: Dict[str, Any] = {
                        "@id": code_id,
                        "@type": "Code",
                        "identifier": str(cat_value)
                    }
                    if cat_label:
                        code_node["name"] = cat_label
                    graph.append(code_node)
                
                graph.append({
                    "@id": codelist_id,
                    "@type": "CodeList",
                    "name": f"{label or col_name} codes",
                    "has_Code": code_ids
                })
            
            # Create SubstantiveValueDomain
            domain_node: Dict[str, Any] = {
                "@id": domain_id,
                "@type": "SubstantiveValueDomain",
                "recommendedDataType": col_stats.ddi_datatype_uri()
            }
            if codelist_id:
                domain_node["takesValuesFrom"] = codelist_id
            graph.append(domain_node)
            
            # Create Variable (InstanceVariable + RepresentedVariable)
            var_node: Dict[str, Any] = {
                "@id": var_id,
                "@type": ["InstanceVariable", "RepresentedVariable"],
                "name": label or col_name,
                "takesSubstantiveValuesFrom_SubstantiveValueDomain": domain_id
            }
            if label and label != col_name:
                var_node["definition"] = f"Column: {col_name}"
            graph.append(var_node)
            
            # Create CategoryStatistic nodes for summary statistics
            if statistics:
                for stat_type, stat_value in statistics.items():
                    stat_frag = safe_fragment(stat_type)
                    stat_id = f"#{full_frag}_stat_{stat_frag}"
                    
                    # Try to parse numeric value
                    try:
                        numeric_value = float(stat_value)
                    except (ValueError, TypeError):
                        numeric_value = None
                    
                    stat_node: Dict[str, Any] = {
                        "@id": stat_id,
                        "@type": "CategoryStatistic",
                        "appliesTo": var_id,
                        "typeOfCategoryStatistic": stat_type
                    }
                    
                    if numeric_value is not None:
                        stat_node["statistic"] = {
                            "@type": "Statistic",
                            "content": numeric_value
                        }
                    else:
                        # Keep as string if not numeric
                        stat_node["statistic"] = {
                            "@type": "Statistic",
                            "content": str(stat_value)
                        }
                    
                    graph.append(stat_node)
            
            # Create Component
            graph.append({
                "@id": component_id,
                "@type": component_type,
                "isDefinedBy_RepresentedVariable": var_id
            })
            
            # Create ValueMapping
            graph.append({
                "@id": mapping_id,
                "@type": "ValueMapping",
                "defaultValue": ""
            })
            
            # Create ValueMappingPosition
            graph.append({
                "@id": mapping_pos_id,
                "@type": "ValueMappingPosition",
                "indexes": mapping_id
            })
    
    # Create WideDataSet (root)
    dataset_node: Dict[str, Any] = {
        "@id": dataset_id,
        "@type": "WideDataSet",
        "isStructuredBy": structure_id
    }
    # Link to CatalogDetails if we have rich metadata
    if rich_metadata:
        dataset_node["catalogDetails"] = catalog_details_id
    # Name is also at dataset level for simple access
    dataset_node["name"] = dataset_title
    if dataset_description:
        dataset_node["description"] = dataset_description
    if dataset_pid:
        dataset_node["identifier"] = dataset_pid
    if files_data and files_data[0].get("file_name"):
        dataset_node["physicalFileName"] = files_data[0]["file_name"]
    graph.append(dataset_node)
    
    # Create WideDataStructure
    graph.append({
        "@id": structure_id,
        "@type": "WideDataStructure",
        "has_DataStructureComponent": all_component_ids
    })
    
    # Create LogicalRecord
    graph.append({
        "@id": logical_record_id,
        "@type": "LogicalRecord",
        "organizes": dataset_id,
        "has_InstanceVariable": all_variable_ids
    })
    
    # Create PrimaryKey if we have identifier components
    if primary_key_components:
        primary_key_id = "#primaryKey"
        primary_key_component_id = "#primaryKeyComponent"
        
        graph.append({
            "@id": primary_key_id,
            "@type": "PrimaryKey",
            "isComposedOf": primary_key_component_id
        })
        
        # Link primary key component to first identifier
        graph.append({
            "@id": primary_key_component_id,
            "@type": "PrimaryKeyComponent",
            "correspondsTo": primary_key_components[0]
        })
        
        # Add primary key to structure
        all_component_ids.insert(0, primary_key_id)
    
    # Create PhysicalSegmentLayout
    physical_layout: Dict[str, Any] = {
        "@id": physical_layout_id,
        "@type": "PhysicalSegmentLayout",
        "formats": logical_record_id,
        "isDelimited": True,
        "hasHeader": True,
        "headerRowCount": 1,
        "has_ValueMapping": all_value_mappings,
        "has_ValueMappingPosition": all_value_mapping_positions
    }
    
    # Add delimiter info from first file
    if files_data:
        first_file = files_data[0]
        file_format = first_file.get("file_format", "text/csv")
        if "tab" in file_format.lower() or first_file.get("file_name", "").endswith((".tsv", ".tab")):
            physical_layout["delimiter"] = "\\t"
        else:
            physical_layout["delimiter"] = ","
    
    graph.append(physical_layout)
    
    # Build final JSON-LD document
    return {
        "@context": DDI_CDI_CONTEXT,
        "@graph": graph
    }


def generate_manifest_jsonld(
    manifest: Dict[str, Any],
    output_path: Path,
    summary_json: Optional[Path],
    skip_md5_default: bool = False,
    quiet: bool = False,
) -> Tuple[List[str], int, int]:
    """Generate JSON-LD output for a dataset manifest.

    Returns a tuple of (warnings, total_rows_processed, files_processed).
    """
    warnings: List[str] = []

    dataset_pid = manifest.get("dataset_pid")
    dataset_uri_base = manifest.get("dataset_uri_base")
    if not dataset_pid or not dataset_uri_base:
        raise ValueError("manifest requires 'dataset_pid' and 'dataset_uri_base'")

    dataset_title = manifest.get("dataset_title")
    dataset_metadata_path = manifest.get("dataset_metadata_path")

    metadata_payload: Optional[Dict[str, Any]] = None
    dataset_description: Optional[str] = None
    rich_metadata: Optional[Dict[str, Any]] = None
    
    if dataset_metadata_path:
        metadata_payload = load_metadata_from_file(Path(dataset_metadata_path))
        if metadata_payload is None:
            warnings.append(f"Failed to parse dataset metadata from {dataset_metadata_path}")

    if metadata_payload:
        # Extract rich metadata for CatalogDetails
        rich_metadata = extract_rich_metadata(metadata_payload)
        
        # Use title from rich metadata if available
        if not dataset_title and rich_metadata.get("title"):
            dataset_title = rich_metadata["title"]
        elif not dataset_title:
            dataset_title = extract_dataset_title(metadata_payload)
        
        # Extract description
        dataset_description = extract_dataset_description(metadata_payload)

    if not dataset_title:
        dataset_title = dataset_pid  # Fallback to PID as title

    files_cfg = manifest.get("files") or []
    if not files_cfg:
        raise ValueError("manifest contains no files to process")

    files_data: List[Dict[str, Any]] = []
    summary_payload: List[Dict[str, Any]] = []
    total_rows = 0

    for file_cfg in files_cfg:
        if "csv_path" not in file_cfg:
            raise ValueError("each manifest file entry must include 'csv_path'")

        csv_path = Path(file_cfg["csv_path"])
        if not csv_path.exists():
            raise FileNotFoundError(f"CSV path not found: {csv_path}")

        file_name = file_cfg.get("file_name") or csv_path.name
        header_option = file_cfg.get("header", "auto")
        delimiter = file_cfg.get("delimiter")
        encoding = file_cfg.get("encoding")
        limit_rows = file_cfg.get("limit_rows")
        skip_md5 = file_cfg.get("skip_md5", skip_md5_default)
        allow_xconvert = file_cfg.get("allow_xconvert", True)

        file_uri = file_cfg.get("file_uri")
        
        ddi_path_value = file_cfg.get("ddi_path")
        ddi_path: Optional[Path] = None
        if ddi_path_value:
            ddi_candidate = Path(ddi_path_value)
            if ddi_candidate.exists():
                ddi_path = ddi_candidate
            else:
                warnings.append(f"DDI metadata file missing for {file_name}: {ddi_candidate}")

        if ddi_path is None and allow_xconvert:
            xconvert_ddi = detect_and_run_xconvert(csv_path, csv_path.parent)
            if xconvert_ddi:
                ddi_path = xconvert_ddi

        ddi_raw: Optional[str] = None
        ddi_variables: Dict[str, Dict[str, Any]] = {}
        if ddi_path:
            ddi_raw, ddi_variables, _ = load_ddi_metadata(ddi_path)
            if not ddi_raw:
                warnings.append(f"DDI metadata unavailable or invalid for {file_name}: {ddi_path}")

        columns, stats, info, file_md5_value, dialect = stream_profile_csv(
            csv_path,
            encoding=encoding,
            delimiter=delimiter,
            header=header_option,
            limit_rows=limit_rows,
            compute_md5=not skip_md5,
        )

        # Determine file format
        file_format = "text/csv"
        if file_name.endswith(".tsv") or file_name.endswith(".tab"):
            file_format = "text/tab-separated-values"
        elif dialect.delimiter == "\t":
            file_format = "text/tab-separated-values"

        files_data.append({
            "file_name": file_name,
            "file_uri": file_uri,
            "file_format": file_format,
            "file_md5": file_md5_value,
            "columns": columns,
            "stats": stats,
            "ddi_variables": ddi_variables,
        })

        total_rows += info.get("rows_read", 0)

        # Build summary
        column_entries: List[Dict[str, Any]] = []
        for name, st in zip(columns, stats):
            entry = {
                "name": name,
                "datatype": st.xsd_datatype_name(),
                "role": st.role(),
                "approx_distinct": st.approx_distinct(),
                "non_missing": st.n_non_missing,
            }
            ddi_info = ddi_variables.get(name, {}) if isinstance(ddi_variables, dict) else {}
            ddi_label = ddi_info.get("label") if isinstance(ddi_info, dict) else None
            if ddi_label:
                entry["ddi_label"] = ddi_label
            column_entries.append(entry)

        summary_payload.append({
            "file": file_name,
            "rows_profiled": info.get("rows_read", 0),
            "columns": column_entries,
        })

    # Build JSON-LD
    jsonld_doc = build_jsonld_graph(
        dataset_title=dataset_title,
        dataset_description=dataset_description,
        files_data=files_data,
        dataset_pid=dataset_pid,
        rich_metadata=rich_metadata,
    )

    # Write output
    output_parent = output_path.parent
    if output_parent != Path("."):
        output_parent.mkdir(parents=True, exist_ok=True)
    
    output_path.write_text(
        json.dumps(jsonld_doc, indent=2, ensure_ascii=False),
        encoding="utf-8"
    )
    
    if not quiet:
        logging.info("Wrote DDI-CDI JSON-LD to %s", output_path)

    if summary_json:
        summary = {
            "dataset_pid": dataset_pid,
            "rows_profiled": total_rows,
            "files": summary_payload,
        }
        summary_json.parent.mkdir(parents=True, exist_ok=True)
        summary_json.write_text(json.dumps(summary, indent=2), encoding="utf-8")

    return warnings, total_rows, len(files_cfg)


# ------------------------------ xconvert support ------------------------------

def run_xconvert(data_file: Path, syntax_file: Path, work_dir: Path) -> Optional[Path]:
    """Run xconvert to generate DDI XML from a syntax file."""
    import subprocess
    
    ext = syntax_file.suffix.lower()
    xconvert_format_map = {
        ".sps": "spss",
        ".sas": "sas",
        ".do": "stata",
        ".dct": "stata",
    }
    
    if ext not in xconvert_format_map:
        logging.warning(f"Unsupported syntax file type: {ext}")
        return None
    
    format_name = xconvert_format_map[ext]
    ddi_output = work_dir / f"xconvert_{syntax_file.stem}.xml"
    
    try:
        cmd = ["xconvert", "-x", format_name, "-y", "ddi", "-i", str(syntax_file), "-o", str(ddi_output)]
        logging.info(f"Running xconvert: {' '.join(cmd)}")
        
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=300,
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
    """Detect if there's a matching syntax file for the data file and run xconvert."""
    parent = csv_path.parent
    stem = csv_path.stem
    
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
    p = argparse.ArgumentParser(description="Stream a CSV and emit DDI-CDI JSON-LD.")
    p.add_argument("--manifest", type=Path, help="Path to dataset manifest JSON (enables multi-file mode)")
    p.add_argument("--csv", type=Path, help="Path to CSV file (legacy single-file mode)")
    p.add_argument("--dataset-pid", help="Dataset PID/DOI (required in single-file mode)")
    p.add_argument("--dataset-uri-base", help="Base URI for dataset landing pages (single-file mode)")
    p.add_argument("--file-uri", help="Public URI for this data file (if any; single-file mode)")
    p.add_argument("--dataset-title", help="Dataset title (optional; single-file mode)")
    p.add_argument("--dataset-metadata-file", type=Path, help="Optional path to Dataverse dataset metadata JSON")
    p.add_argument("--output", "-o", type=Path, default=Path("dataset.cdi.jsonld"), help="Output JSON-LD path")
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
    if args.manifest and args.csv:
        print("[ERROR] --manifest and --csv are mutually exclusive", file=sys.stderr)
        sys.exit(2)

    if not args.manifest and not args.csv:
        print("[ERROR] Provide either --manifest for multi-file mode or --csv for single-file mode", file=sys.stderr)
        sys.exit(2)

    setup_logging(args.verbose, args.quiet)

    try:
        if args.manifest:
            if not args.manifest.exists():
                raise FileNotFoundError(f"Manifest file not found: {args.manifest}")
            manifest_data = json.loads(args.manifest.read_text(encoding="utf-8"))
            warnings, total_rows, file_count = generate_manifest_jsonld(
                manifest=manifest_data,
                output_path=args.output,
                summary_json=args.summary_json,
                skip_md5_default=args.skip_md5,
                quiet=args.quiet,
            )
            for message in warnings:
                logging.warning(message)
            if not args.quiet:
                print(f"[OK] Wrote DDI-CDI JSON-LD: {args.output}")
                print(f"  files_processed={file_count}, rows_profiled={total_rows}")
                if args.summary_json:
                    print(f"  summary_json={args.summary_json}")
            logging.info("Manifest conversion completed: files=%s rows=%s", file_count, total_rows)
            return

        # Single-file mode
        missing_args = [
            name for name in ("dataset_pid", "dataset_uri_base")
            if not getattr(args, name.replace("-", "_"))
        ]
        if missing_args:
            raise ValueError(
                "Missing required arguments for single-file mode: " + ", ".join(missing_args)
            )
        if not args.csv:
            raise ValueError("--csv is required in single-file mode")
        if not args.csv.exists():
            raise FileNotFoundError(f"CSV path not found: {args.csv}")

        # Load metadata from file if provided
        metadata_payload: Optional[Dict[str, Any]] = None
        if args.dataset_metadata_file:
            metadata_payload = load_metadata_from_file(args.dataset_metadata_file)

        if metadata_payload and not args.dataset_title:
            inferred_title = extract_dataset_title(metadata_payload)
            if inferred_title:
                args.dataset_title = inferred_title

        dataset_description: Optional[str] = None
        rich_metadata: Optional[Dict[str, Any]] = None
        if metadata_payload:
            dataset_description = extract_dataset_description(metadata_payload)
            rich_metadata = extract_rich_metadata(metadata_payload)

        ddi_variables: Dict[str, Dict[str, Any]] = {}

        if not args.ddi_file:
            logging.info("No DDI file provided, checking for xconvert-compatible syntax files")
            work_path = Path(args.csv).parent
            xconvert_ddi = detect_and_run_xconvert(args.csv, work_path)
            if xconvert_ddi:
                args.ddi_file = xconvert_ddi
                logging.info("Using xconvert-generated DDI: %s", xconvert_ddi)

        if args.ddi_file:
            raw_ddi, parsed_variables, _ = load_ddi_metadata(args.ddi_file)
            if raw_ddi:
                ddi_variables = parsed_variables
                logging.info("Loaded DDI fragment from %s", args.ddi_file)

        logging.info("Starting CSV to DDI-CDI JSON-LD conversion")
        logging.info("Input file: %s", args.csv)
        logging.info("Output file: %s", args.output)

        header_mode: Union[bool, str]
        if args.no_header:
            header_mode = "absent"
        else:
            header_mode = "auto"

        cols, stats, info, md5, dialect = stream_profile_csv(
            args.csv,
            encoding=args.encoding,
            delimiter=args.delimiter,
            header=header_mode,
            limit_rows=args.limit_rows,
            compute_md5=not args.skip_md5,
        )

        # Determine file format
        file_format = "text/csv"
        if args.csv.suffix.lower() in (".tsv", ".tab"):
            file_format = "text/tab-separated-values"
        elif dialect.delimiter == "\t":
            file_format = "text/tab-separated-values"

        files_data = [{
            "file_name": args.csv.name,
            "file_uri": args.file_uri,
            "file_format": file_format,
            "file_md5": md5,
            "columns": cols,
            "stats": stats,
            "ddi_variables": ddi_variables,
        }]

        dataset_title = args.dataset_title or args.dataset_pid

        jsonld_doc = build_jsonld_graph(
            dataset_title=dataset_title,
            dataset_description=dataset_description,
            files_data=files_data,
            dataset_pid=args.dataset_pid,
            rich_metadata=rich_metadata,
        )

        # Write output
        output_path = args.output
        output_parent = output_path.parent
        if output_parent != Path("."):
            output_parent.mkdir(parents=True, exist_ok=True)
        
        output_path.write_text(
            json.dumps(jsonld_doc, indent=2, ensure_ascii=False),
            encoding="utf-8"
        )

        if not args.quiet:
            print(f"[OK] Wrote DDI-CDI JSON-LD: {args.output}")
            print(f"  rows_profiled={info['rows_read']}, columns={len(cols)}")

        if not args.quiet:
            print("\nColumn Analysis:")
        for name, st in zip(cols, stats):
            datatype_name = st.xsd_datatype_name()
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

        if args.summary_json:
            summary_path = args.summary_json
            summary_parent = summary_path.parent
            if summary_parent != Path("."):
                summary_parent.mkdir(parents=True, exist_ok=True)
            
            column_summaries = []
            for name, st in zip(cols, stats):
                entry = {
                    "name": name,
                    "datatype": st.xsd_datatype_name(),
                    "role": st.role(),
                    "approx_distinct": st.approx_distinct(),
                    "non_missing": st.n_non_missing,
                }
                ddi_info = ddi_variables.get(name, {}) if isinstance(ddi_variables, dict) else {}
                if isinstance(ddi_info, dict) and ddi_info.get("label"):
                    entry["ddi_label"] = ddi_info["label"]
                column_summaries.append(entry)
            
            summary_payload = {
                "dataset_pid": args.dataset_pid,
                "rows_profiled": info["rows_read"],
                "columns": column_summaries,
            }
            summary_path.write_text(json.dumps(summary_payload, indent=2), encoding="utf-8")
            logging.info("Wrote column summary JSON to %s", summary_path)

        logging.info("Conversion completed successfully")

    except Exception as e:
        logging.error("Error during conversion: %s", e)
        print(f"[ERROR] {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
