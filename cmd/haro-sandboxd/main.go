package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandboxd"
)

func main() {
	listen := flag.String("listen", ":8888", "HTTPS listen address")
	workspace := flag.String("workspace", "/workspace", "persistent workspace root")
	certFile := flag.String("tls-cert", "", "server certificate file")
	keyFile := flag.String("tls-key", "", "server private key file")
	clientCAFile := flag.String("client-ca", "", "client CA file")
	tokenFile := flag.String("token-file", "", "bearer token file")
	flag.Parse()

	tokenBytes, err := os.ReadFile(*tokenFile)
	if err != nil {
		log.Fatalf("read runtime token: %v", err)
	}
	clientCA, err := os.ReadFile(*clientCAFile)
	if err != nil {
		log.Fatalf("read client CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(clientCA) {
		log.Fatal("client CA contains no certificates")
	}
	runtime, err := sandboxd.New(*workspace, strings.TrimSpace(string(tokenBytes)))
	if err != nil {
		log.Fatalf("initialize runtime: %v", err)
	}
	server := &http.Server{
		Addr: *listen, Handler: runtime.Handler(), ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots},
	}
	log.Printf("haro-sandboxd listening on %s", *listen)
	if err := server.ListenAndServeTLS(*certFile, *keyFile); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
