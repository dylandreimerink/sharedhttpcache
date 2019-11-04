package sharedhttpcache_test

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"

	"github.com/dylandreimerink/sharedhttpcache/layer"
	"golang.org/x/net/http2"

	"github.com/dylandreimerink/sharedhttpcache"
)

// Example demonstrate the most basic setup for a cache controller where a single origin server is used
func Example() {

	controller := &sharedhttpcache.CacheController{
		DefaultForwardConfig: &sharedhttpcache.ForwardConfig{
			Host: "example.com",
			TLS:  true,
		},
		Layers: []layer.CacheLayer{
			layer.NewInMemoryCacheLayer(128 * 1024 * 1024), // 128MB of in-memory(RAM) storage
		},
	}

	server := &http.Server{
		Handler: controller,
	}

	err := server.ListenAndServe()
	if err != nil {
		fmt.Printf("Server exited with error: %s", err.Error())
	}
}

//ExampleMultiOrigin demonstrates how the cache can forward to multiple origin server.
//The decision which origin server to use in this example is based on the Host header in the request
func Example_multiOrigin() {

	originServers := map[string]*sharedhttpcache.ForwardConfig{
		"example.com": &sharedhttpcache.ForwardConfig{
			Host: "2606:2800:220:1:248:1893:25c8:1946",
			TLS:  true,
		},
		"theuselessweb.com": &sharedhttpcache.ForwardConfig{
			Host: "3.121.157.244",
			TLS:  false,
		},
	}

	controller := &sharedhttpcache.CacheController{
		ForwardConfigResolver: sharedhttpcache.ForwardConfigResolverFunc(func(req *http.Request) *sharedhttpcache.ForwardConfig {
			return originServers[req.Host]
		}),
		Layers: []layer.CacheLayer{
			layer.NewInMemoryCacheLayer(128 * 1024 * 1024), // 128MB of in-memory(RAM) storage
		},
	}

	server := &http.Server{
		Handler: controller,
	}

	err := server.ListenAndServe()
	if err != nil {
		fmt.Printf("Server exited with error: %s", err.Error())
	}
}

//ExampleHTTP2 demonstrates how to enable HTTP/2 on the connection from the client to the cache server and from the cache server to the origin.
//It is not required to have both connections support HTTP/2 one or the other will also work
func Example_http2() {

	systemCertPool, err := x509.SystemCertPool()
	if err != nil {
		panic(err)
	}

	http2Transport := &http2.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: systemCertPool,
		},
	}

	controller := &sharedhttpcache.CacheController{
		DefaultForwardConfig: &sharedhttpcache.ForwardConfig{
			Host: "example.com",
			TLS:  true,
		},
		DefaultTransport: http2Transport,
		Layers: []layer.CacheLayer{
			layer.NewInMemoryCacheLayer(128 * 1024 * 1024), // 128MB of in-memory(RAM) storage
		},
	}

	certPem := []byte(`-----BEGIN CERTIFICATE-----
MIICGTCCAcCgAwIBAgIIelEInVZKyIUwCgYIKoZIzj0EAwIwVDELMAkGA1UEBhMC
TkwxEjAQBgNVBAMMCWxvY2FsaG9zdDETMBEGA1UECAwKU29tZS1TdGF0ZTEcMBoG
A1UECgwTRGVmYXVsdCBDb21wYW55IEx0ZDAeFw0xOTExMDQyMjUwMTVaFw0yOTEx
MDEyMjUwMTVaMFQxCzAJBgNVBAYTAk5MMRIwEAYDVQQDDAlsb2NhbGhvc3QxEzAR
BgNVBAgMClNvbWUtU3RhdGUxHDAaBgNVBAoME0RlZmF1bHQgQ29tcGFueSBMdGQw
WTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAAQw8ez8+AGVCZWeWXRBy5EvbeqBVQ1G
8o6zYzVhgN13tY0sCKYaBro7oooyGat1JkcE7CoPE3Gv4n+hOujCd9Yto3wwejAd
BgNVHQ4EFgQUUbULEj2qxKQ7NHcIS1XSw+wPtMEwHwYDVR0jBBgwFoAUUbULEj2q
xKQ7NHcIS1XSw+wPtMEwDAYDVR0TBAUwAwEB/zALBgNVHQ8EBAMCA6gwHQYDVR0l
BBYwFAYIKwYBBQUHAwIGCCsGAQUFBwMBMAoGCCqGSM49BAMCA0cAMEQCIAEzF98B
zeZlCSU4EFZO5ZaO+YrqJGQOppWOtQcZgei7AiAS5E2ZhciP5xQEjmE0j3D9CNSG
7DrIoXvr+qKh/1/hcA==
-----END CERTIFICATE-----`)
	keyPem := []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEII7nelmettK1JFHWoSmVHLGa0ho/2nE6bQbiASvKqQXHoAoGCCqGSM49
AwEHoUQDQgAEMPHs/PgBlQmVnll0QcuRL23qgVUNRvKOs2M1YYDdd7WNLAimGga6
O6KKMhmrdSZHBOwqDxNxr+J/oTrownfWLQ==
-----END EC PRIVATE KEY-----`)

	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:    ":4443",
		Handler: controller,

		//Use a secure TLS config (As of 2019)
		TLSConfig: &tls.Config{
			Certificates:             []tls.Certificate{cert},
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			},
		},
	}

	err = http2.ConfigureServer(server, nil)
	if err != nil {
		panic(err)
	}

	err = server.ListenAndServeTLS("", "")
	if err != nil {
		fmt.Printf("Server exited with error: %s", err.Error())
	}
}
