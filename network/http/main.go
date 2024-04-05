package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func normalCacheHandler(w http.ResponseWriter, r *http.Request) {
 	w.Header().Set("Cache-Control", "max-age=3600")
	w.Write([]byte("hello world"))
}

func etagCacheHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("If-None-Match") == "1234" {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Etag", "1234")
	w.Write([]byte("hello world"))
}

func initFlags() {
	proxyCmd := flag.NewFlagSet("proxy", flag.ExitOnError)
	serverCmd := flag.NewFlagSet("server", flag.ExitOnError)
	switch os.Args[1] {
	case "proxy":
		host := proxyCmd.String("host", ":19090", "listen host")
		proxyCmd.Parse(os.Args[2:])
		runProxyServer(*host)
	case "server":
		host := serverCmd.String("host", ":19090", "listen host")
		serverCmd.Parse(os.Args[2:])
		runServer(*host)
	}
}

func proxyDirector(req *http.Request) {
	req.URL.Scheme = "http"
	log.Println("proxy director", req.URL.String())
}

func copyHeaders(src, dst http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

type ForwardProxy struct{}

func (p *ForwardProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	log.Println("req", req.URL.String())
	client := &http.Client{}
	req.RequestURI = ""
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Server error %s", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	copyHeaders(resp.Header, w.Header())
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func runProxyServer(host string) {
	http.ListenAndServe(host, &ForwardProxy{})
}

type HttpEngine struct {
	middleWares []http.HandlerFunc
	mux         *http.ServeMux
}

func (h *HttpEngine) Use(hf http.HandlerFunc) {
	h.middleWares = append(h.middleWares, hf)
}

func (h *HttpEngine) HandleFunc(pattern string, handler http.HandlerFunc) {
	h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		for _, v := range h.middleWares {
			v(w, r)
		}
		handler(w, r)
	})
}

func (h *HttpEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func HttpLog(w http.ResponseWriter, r *http.Request) {
	log.Println(r.Method, r.URL.String())
}

func runServer(host string) {
	mux := http.NewServeMux()
	engine := HttpEngine{mux: mux}
	engine.Use(HttpLog)
	engine.HandleFunc("/cache/normal", normalCacheHandler)
	engine.HandleFunc("/cache/etag", etagCacheHandler)
	log.Println("http server listen on", host)
	http.ListenAndServe(host, &engine)
}

func main() {
	initFlags()
}
