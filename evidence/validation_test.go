package evidence_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestValidationPathsNeverRenderCausesOrInvalidPathText(t *testing.T) {
	t.Parallel()
	cause := errors.New("PRIVATE_CANARY")
	err := evidence.At("observations[3]", evidence.At("metadata.reference", cause))
	var field *evidence.ValidationError
	if !errors.Is(err, cause) || !errors.As(err, &field) || field.Path != "observations[3].metadata.reference" ||
		strings.Contains(err.Error(), "CANARY") {
		t.Fatal("validation path lost its classification or rendered a cause")
	}
	if got := evidence.At("https://PRIVATE_CANARY", cause).Error(); strings.Contains(got, "CANARY") {
		t.Fatal("invalid path text escaped validation")
	}
	if evidence.At("field", nil) != nil {
		t.Fatal("a valid field acquired an error")
	}
}
