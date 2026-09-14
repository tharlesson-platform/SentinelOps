package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCollectUsesExplicitNamespacesAndRejectsWrites(t *testing.T) {
	cfg := config{Context: "cluster-hml", Namespaces: []string{"apps"}, Environment: "hml", OwnerTeam: "platform"}
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "kubectl" {
			return nil, errors.New("unexpected binary")
		}
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		switch {
		case strings.Contains(joined, "get namespace kube-system"):
			return []byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"uid":"cluster-uid","name":"kube-system"}}`), nil
		case strings.Contains(joined, "auth can-i"):
			if strings.Contains(joined, "get secrets") || strings.Contains(joined, "create deployments") {
				return []byte("no\n"), nil
			}
			return []byte("yes\n"), nil
		case strings.Contains(joined, "get nodes"):
			return []byte(`{"items":[{"apiVersion":"v1","kind":"Node","metadata":{"uid":"node-uid","name":"node-a"}}]}`), nil
		case strings.Contains(joined, "get pods,services,deployments"):
			return []byte(`{"items":[{"apiVersion":"v1","kind":"Pod","metadata":{"uid":"pod-uid","name":"api-1","namespace":"apps"}}]}`), nil
		default:
			return nil, errors.New("unexpected kubectl arguments")
		}
	}
	got, err := collect(context.Background(), cfg, run, time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete || len(got.Assets) != 2 {
		t.Fatalf("unexpected snapshot: %#v", got)
	}
	foundPod := false
	for _, item := range got.Assets {
		foundPod = foundPod || item.Labels["kubernetes_namespace"] == "apps"
	}
	if !foundPod {
		t.Fatalf("namespaced pod missing: %#v", got.Assets)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "get secrets") || !strings.Contains(strings.Join(calls, "\n"), "create deployments") {
		t.Fatal("negative RBAC checks were not executed")
	}
}

func TestCollectRejectsUnexpectedSecretPermission(t *testing.T) {
	cfg := config{Context: "cluster-hml", Namespaces: []string{"apps"}, Environment: "hml", OwnerTeam: "platform"}
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "get namespace kube-system") {
			return []byte(`{"metadata":{"uid":"cluster-uid","name":"kube-system"}}`), nil
		}
		if strings.Contains(joined, "auth can-i get secrets") {
			return []byte("yes\n"), nil
		}
		return []byte("yes\n"), nil
	}
	if _, err := collect(context.Background(), cfg, run, time.Now()); err == nil || !strings.Contains(err.Error(), "RBAC inesperado") {
		t.Fatalf("secret access must fail closed, got %v", err)
	}
}
