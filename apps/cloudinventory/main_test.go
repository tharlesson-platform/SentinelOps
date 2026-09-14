package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCollectAzurePaginatesAndUsesStableID(t *testing.T) {
	cfg := scopeFile{Provider: "azure", Environment: "hml", OwnerTeam: "platform", Scope: cloudScope{SubscriptionID: "11111111-1111-1111-1111-111111111111"}}
	calls := 0
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "az" {
			t.Fatalf("unexpected binary %s", name)
		}
		if strings.Join(args[:2], " ") == "account show" {
			return []byte(`{"id":"11111111-1111-1111-1111-111111111111"}`), nil
		}
		calls++
		if calls == 1 {
			return []byte(`{"data":[{"id":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-a","name":"vm-a","type":"Microsoft.Compute/virtualMachines","location":"brazilsouth","resourceGroup":"rg","subscriptionId":"11111111-1111-1111-1111-111111111111"}],"skipToken":"next"}`), nil
		}
		return []byte(`{"data":[{"id":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/storea","name":"storea","type":"Microsoft.Storage/storageAccounts","location":"brazilsouth","resourceGroup":"rg","subscriptionId":"11111111-1111-1111-1111-111111111111"}]}`), nil
	}
	got, err := collect(context.Background(), cfg, run, time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete || len(got.Assets) != 2 || calls != 2 {
		t.Fatalf("unexpected snapshot: %#v calls=%d", got, calls)
	}
	if got.Assets[0].AssetID == got.Assets[1].AssetID || !strings.HasPrefix(got.Assets[0].AssetID, "azure:") {
		t.Fatalf("unstable Azure identifiers: %#v", got.Assets)
	}
	if got.Assets[0].Labels["azure_resource_id"] == "" {
		t.Fatalf("native Azure resource ID was not preserved: %#v", got.Assets[0].Labels)
	}
}

func TestCollectAWSRejectsOutOfScopeResult(t *testing.T) {
	cfg := scopeFile{Provider: "aws", Environment: "hml", OwnerTeam: "platform", Scope: cloudScope{AccountID: "123456789012", Region: "sa-east-1", Profile: "readonly"}}
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "aws" {
			return nil, errors.New("unexpected binary")
		}
		if strings.Contains(strings.Join(args, " "), "sts get-caller-identity") {
			return []byte(`{"Account":"123456789012"}`), nil
		}
		if strings.Contains(strings.Join(args, " "), "describe-configuration-recorders") {
			return []byte(`{"ConfigurationRecorders":[{"recordingGroup":{"allSupported":true}}]}`), nil
		}
		if strings.Contains(strings.Join(args, " "), "describe-configuration-recorder-status") {
			return []byte(`{"ConfigurationRecordersStatus":[{"recording":true}]}`), nil
		}
		return []byte(`{"Results":["{\"resourceId\":\"i-1\",\"resourceType\":\"AWS::EC2::Instance\",\"resourceName\":\"api\",\"awsRegion\":\"sa-east-1\",\"accountId\":\"999999999999\"}"]}`), nil
	}
	if _, err := collect(context.Background(), cfg, run, time.Now()); err == nil || !strings.Contains(err.Error(), "fora da account") {
		t.Fatalf("expected out-of-scope rejection, got %v", err)
	}
}

func TestLoadScopeRejectsUnknownField(t *testing.T) {
	path := t.TempDir() + "/scope.json"
	if err := os.WriteFile(path, []byte(`{"provider":"azure","environment":"hml","ownerTeam":"platform","scope":{"subscriptionId":"11111111-1111-1111-1111-111111111111"},"unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScope(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("scope with an unknown field must fail, got %v", err)
	}
}

func TestRetryingRetriesTransientFailureButNotAccessDenied(t *testing.T) {
	calls := 0
	run := retrying(func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("rate_limited")
		}
		return []byte(`{}`), nil
	})
	if _, err := run(context.Background(), "az", "graph"); err != nil || calls != 3 {
		t.Fatalf("transient retry result err=%v calls=%d", err, calls)
	}
	calls = 0
	denied := retrying(func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		calls++
		return nil, errors.New("access_denied")
	})
	if _, err := denied(context.Background(), "az", "graph"); err == nil || calls != 1 {
		t.Fatalf("access denied must not retry, err=%v calls=%d", err, calls)
	}
}

func TestClassifyCommandFailureRedactsRawOutput(t *testing.T) {
	err := classifyCommandFailure(errors.New("exit"), []byte(`429 token=do-not-log`))
	if err != "rate_limited" {
		t.Fatalf("unexpected classification %q", err)
	}
}
