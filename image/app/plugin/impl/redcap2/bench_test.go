// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"bytes"
	"fmt"
	"testing"
)

// syntheticFlatCSV builds a wide flat export: rows x cols data cells plus a
// record_id column.
func syntheticFlatCSV(rows, cols int) ([]byte, dictionary) {
	var meta bytes.Buffer
	meta.WriteString("field_name,form_name,field_type,field_label,identifier,text_validation_type_or_show_slider_number\n")
	meta.WriteString("record_id,f,text,Record ID,,\n")
	for c := 0; c < cols; c++ {
		fmt.Fprintf(&meta, "var_%d,f,text,Variable %d,,integer\n", c, c)
	}

	var data bytes.Buffer
	data.WriteString("record_id")
	for c := 0; c < cols; c++ {
		fmt.Fprintf(&data, ",var_%d", c)
	}
	data.WriteByte('\n')
	for r := 0; r < rows; r++ {
		fmt.Fprintf(&data, "%d", r)
		for c := 0; c < cols; c++ {
			fmt.Fprintf(&data, ",%d", r*cols+c)
		}
		data.WriteByte('\n')
	}
	return data.Bytes(), parseDictionary(meta.Bytes())
}

// syntheticEAVCSV builds a long export with rows*cols value rows.
func syntheticEAVCSV(rows, cols int) ([]byte, dictionary) {
	_, dict := syntheticFlatCSV(0, cols)
	var data bytes.Buffer
	data.WriteString("record,field_name,value\n")
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			fmt.Fprintf(&data, "%d,var_%d,%d\n", r, c, r*cols+c)
		}
	}
	return data.Bytes(), dict
}

func benchPlan() transformPlan {
	return testPlan(map[string]string{
		"record_id": "pseudonymize",
		"var_0":     "blank",
		"var_1":     "drop",
	})
}

// BenchmarkTransformFlatCSV measures the full per-export processing cost of a
// 50k-row, 50-column flat CSV (~25 MB class) with pseudonymize+blank+drop.
func BenchmarkTransformFlatCSV(b *testing.B) {
	data, dict := syntheticFlatCSV(50_000, 50)
	plan := benchPlan()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := transformFlatCSV(data, ',', plan, false, dict); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTransformFlatCSVNoRules measures the pass-through cost (parse +
// audit only) on the same input.
func BenchmarkTransformFlatCSVNoRules(b *testing.B) {
	data, dict := syntheticFlatCSV(50_000, 50)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := transformFlatCSV(data, ',', transformPlan{}, false, dict); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTransformEAVCSV measures EAV processing of 2.5M value rows
// (50k records x 50 fields) with record-column pseudonymization.
func BenchmarkTransformEAVCSV(b *testing.B) {
	data, dict := syntheticEAVCSV(50_000, 50)
	plan := benchPlan()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := transformEAVCSV(data, ',', plan, dict); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSidecars measures generating all three sidecars for a 500-variable
// dictionary (large-project class).
func BenchmarkSidecars(b *testing.B) {
	data, dict := syntheticFlatCSV(100, 500)
	opts, _ := parsePluginOptions(`{"exportMode":"records"}`)
	files := map[string][]byte{"redcap/records/data.csv": data}
	model := buildSidecarModel(opts, transformPlan{}, dict, "redcap/records", files, "redcap/records/data.csv", "14.5.5", float64(1), "Bench")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mime := map[string]string{}
		bundle := map[string][]byte{"redcap/records/data.csv": data}
		if warnings := addSidecars(model, "redcap/records", bundle, mime); len(warnings) != 0 {
			b.Fatalf("sidecar warnings: %v", warnings)
		}
	}
}
