package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "listen address")
	flag.Parse()

	srv := &http.Server{Addr: *addr, Handler: fakekodi.NewServer(fakekodi.Options{}), ReadHeaderTimeout: 10 * time.Second}

	fmt.Fprintf(os.Stderr, "fake Kodi listening on http://%s/jsonrpc\n", *addr)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
