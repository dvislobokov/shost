package httpsvc_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
	"github.com/dvislobokov/shost/httpsvc"
)

func TestServeAndGracefulShutdown(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pong")
	})
	svc := httpsvc.New("127.0.0.1:0", mux, httpsvc.WithName("api"))

	h := shost.New().AddService(svc).MustBuild()
	res := make(chan error, 1)
	go func() { res <- h.RunContext(context.Background()) }()

	select {
	case <-svc.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("server did not become ready")
	}

	resp, err := http.Get("http://" + svc.Addr() + "/ping")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "pong" {
		t.Fatalf("unexpected response: %d %q", resp.StatusCode, body)
	}

	h.Shutdown()
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("expected clean shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host did not stop")
	}
}

func TestListenErrorStopsHost(t *testing.T) {
	svc := httpsvc.New("256.256.256.256:0", http.NewServeMux())
	h := shost.New().AddService(svc).MustBuild()

	res := make(chan error, 1)
	go func() { res <- h.RunContext(context.Background()) }()
	select {
	case err := <-res:
		if err == nil {
			t.Fatal("expected listen error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host did not stop")
	}
}

func TestSlowRequestIsDrainedOnShutdown(t *testing.T) {
	requestDone := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "done")
		close(requestDone)
	})
	svc := httpsvc.New("127.0.0.1:0", mux)
	h := shost.New().WithShutdownTimeout(3 * time.Second).AddService(svc).MustBuild()

	res := make(chan error, 1)
	go func() { res <- h.RunContext(context.Background()) }()
	<-svc.Ready()

	go http.Get("http://" + svc.Addr() + "/slow")
	time.Sleep(20 * time.Millisecond) // let the request reach the handler
	h.Shutdown()

	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("expected clean shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host did not stop")
	}
	select {
	case <-requestDone:
	default:
		t.Fatal("in-flight request was not drained before shutdown completed")
	}
}
