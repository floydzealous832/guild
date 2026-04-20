package main

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestAnalyzer_Bad verifies that the analyzer fires on every expected
// diagnostic in testdata/src/bad/bad.go.  Each expected diagnostic is
// annotated inline with a "// want `pattern`" comment.
func TestAnalyzer_Bad(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, Analyzer, "bad")
}

// TestAnalyzer_Good verifies that the analyzer produces zero diagnostics
// for the patterns in testdata/src/good/good.go.  analysistest.Run fails
// the test if any unexpected diagnostic appears.
func TestAnalyzer_Good(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, Analyzer, "good")
}
