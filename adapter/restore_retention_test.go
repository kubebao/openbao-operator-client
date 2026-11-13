package adapter_test

import (
	"bytes"
	"testing"

	"github.com/kubebao/openbao-operator-client/adapter"
	"github.com/kubebao/openbao-operator-client/evidence"
)

func TestRestoreRetentionProjectsOnlyReportedBoundedFields(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{true, false} {
		for _, tc := range []struct {
			name, spec, result string
			lifecycle, seconds evidence.ValueState
			terminal           evidence.ValueState
			wantSeconds        int64
		}{
			{
				"reported", `"targetLifecycle":"Disposable","cleanupAfterSeconds":3600`, `"Succeeded"`,
				evidence.ValueReported, evidence.ValueReported, evidence.ValueReported, 3600,
			},
			{
				"maximum", `"targetLifecycle":"Disposable","cleanupAfterSeconds":86400`, `"Succeeded"`,
				evidence.ValueReported, evidence.ValueReported, evidence.ValueReported, 86400,
			},
			{
				"zero", `"targetLifecycle":"Disposable","cleanupAfterSeconds":0`, `"Succeeded"`,
				evidence.ValueReported, evidence.ValueReported, evidence.ValueReported, 0,
			},
			{"omitted", `"source":{"key":"SOURCE_CANARY"}`, ``, evidence.ValueUnknown, evidence.ValueUnknown, evidence.ValueUnknown, 0},
			{
				"retained", `"targetLifecycle":"Retain"`, `"Failed"`,
				evidence.ValueReported, evidence.ValueUnknown, evidence.ValueReported, 0,
			},
			{
				"negative", `"targetLifecycle":"Disposable","cleanupAfterSeconds":-1`, `"Succeeded"`,
				evidence.ValueReported, evidence.ValueInvalid, evidence.ValueReported, 0,
			},
			{
				"too-large", `"targetLifecycle":"Disposable","cleanupAfterSeconds":86401`, `"Succeeded"`,
				evidence.ValueReported, evidence.ValueInvalid, evidence.ValueReported, 0,
			},
			{
				"fractional", `"targetLifecycle":"Disposable","cleanupAfterSeconds":0.5`, `"Succeeded"`,
				evidence.ValueReported, evidence.ValueInvalid, evidence.ValueReported, 0,
			},
			{
				"null", `"targetLifecycle":null,"cleanupAfterSeconds":null`, `null`,
				evidence.ValueInvalid, evidence.ValueInvalid, evidence.ValueInvalid, 0,
			},
			{
				"provider-text", `"targetLifecycle":"LIFECYCLE_CANARY","cleanupAfterSeconds":"SECONDS_CANARY"`, `"RESULT_CANARY"`,
				evidence.ValueInvalid, evidence.ValueInvalid, evidence.ValueInvalid, 0,
			},
		} {
			options := projectionOptions()
			if native {
				options = nativeOptions()
			}
			t.Run(string(options.Contract)+"/"+tc.name, func(t *testing.T) {
				raw := replaceSection(t, restoreFixture(t), "spec", `{"cluster":"bao",`+tc.spec+`}`)
				if tc.result != "" {
					raw = bytes.Replace(raw, []byte(`"stage":"Committed"`), []byte(`"stage":"Unknown","terminalResult":`+tc.result), 1)
				}
				if native {
					raw = nativeDocument(raw)
				}
				observed, err := adapter.ProjectRestore(raw, options)
				if err != nil || observed.TargetLifecycle.State != tc.lifecycle || observed.CleanupAfterSeconds.State != tc.seconds ||
					observed.Execution.TerminalResult.State != tc.terminal {
					t.Fatalf("restore retention field projection changed presence or bounds: %v", err)
				}
				if tc.seconds == evidence.ValueReported && *observed.CleanupAfterSeconds.Value != tc.wantSeconds {
					t.Fatal("reported cleanup seconds changed value")
				}
				assertSafeReplay(t, observed)
			})
		}
	}
}

func TestRestoreRetentionContractsRejectPreviousProjectionIDs(t *testing.T) {
	t.Parallel()
	for _, previous := range []adapter.Contract{
		"openbao.org/v1alpha1@9671c306/maintenance-pools-v1",
		"kubebao.org/v1alpha1@22894cdc/maintenance-pools-v1",
		"openbao.org/v1alpha1/unqualified-v3",
		"kubebao.org/v1alpha1/unqualified-v1",
	} {
		_, err := adapter.Describe(previous)
		assertProjectionError(t, err, adapter.UnsupportedContract)
	}
}
