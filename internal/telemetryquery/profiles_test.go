package telemetryquery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProfileRPCUsesFixedMethodTenantMillisecondsAndDecodesDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/querier.v1.QuerierService/SelectMergeStacktraces" || r.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Error("invalid RPC")
		}
		fmt.Fprint(w, `{"flamegraph":{"names":["root","left","right"],"levels":[{"values":["0","100","0","0"]},{"values":["10","20","10","1","5","30","10","2"]}],"total":"100"}}`)
	}))
	defer srv.Close()
	c := &Client{httpClient: srv.Client(), backends: map[string]string{"pyroscope": srv.URL}}
	got, err := c.ProfileFlamegraph(context.Background(), "tenant-a", "process_cpu:cpu:nanoseconds:cpu:nanoseconds", `{service_name="api"}`, time.Unix(1000, 0), time.Unix(2000, 0))
	if err != nil || len(got.Frames) != 3 || got.Frames[2].Offset != "35" || got.Frames[2].Total != "30" || got.Total != "100" {
		t.Fatalf("decode: %v %v", got, err)
	}
	if err = c.profileRPC(context.Background(), "tenant-a", "Push", nil, nil); err == nil {
		t.Fatal("ingestion method accepted")
	}
}
