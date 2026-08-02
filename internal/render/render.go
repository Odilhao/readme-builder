package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/Odilhao/readme-builder/internal/model"
)

// Render executes the template at templatePath against data and writes the
// result to outputPath. If outputPath is "-", output goes to stdout.
// Output is written atomically via a temporary file and rename.
// Invariant 5: uses text/template, never html/template.
func Render(data *model.Data, templatePath, outputPath string) error {
	tmpl, err := template.New(filepath.Base(templatePath)).
		Funcs(FuncMap(data.Now)).
		ParseFiles(templatePath)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	// Handle stdout special case
	if outputPath == "-" {
		_, err := out.WriteTo(os.Stdout)
		return err
	}

	// Write atomically via temp file + rename
	tmpFile, err := os.CreateTemp(filepath.Dir(outputPath), ".tmp-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(out.Bytes()); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// Check renders the template and compares the output to the file at outputPath.
// It returns true if the rendered output differs from the file (drift detected).
// If the file doesn't exist, drift is reported as true.
// It does not write anything.
func Check(data *model.Data, templatePath, outputPath string) (bool, error) {
	tmpl, err := template.New(filepath.Base(templatePath)).
		Funcs(FuncMap(data.Now)).
		ParseFiles(templatePath)
	if err != nil {
		return false, fmt.Errorf("parse template: %w", err)
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return false, fmt.Errorf("execute template: %w", err)
	}

	// Read existing output
	existing, err := os.ReadFile(outputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // File doesn't exist = drift
		}
		return false, fmt.Errorf("read output file: %w", err)
	}

	// Compare
	if !bytes.Equal(out.Bytes(), existing) {
		return true, nil // Drift detected
	}

	return false, nil // No drift
}

// DumpJSON marshals the entire data model to JSON bytes.
// This is used for the GitHub Pages pipeline and the -dump-json CLI flag.
func DumpJSON(data *model.Data) ([]byte, error) {
	return json.MarshalIndent(data, "", "  ")
}
