package semantic

import _ "embed"

//go:embed scripts/semantic_matcher.py
var embeddedPythonScript string

//go:embed scripts/requirements.txt
var embeddedRequirements string

const defaultRequirements = `sentence-transformers>=2.2.2
torch>=2.0.0
numpy>=1.21.0`
