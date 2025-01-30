package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
)

func HelloFunc(writer http.ResponseWriter, request *http.Request) {
	for n, v := range request.Header {
		log.Printf("%v => %v", n, v)
	}
	reqDump, err := httputil.DumpRequest(request, true)
	if err != nil {
		log.Println("dump request error", err)
		return
	}

	log.Printf("REQUEST:\n%s", string(reqDump))
	//data := fmt.Sprintf("hello %v https", request.URL.Path)
	fmt.Println(request.Header)
	//data = fmt.Sprintf("%v\nheaders")
	//for k, v := range request.Header {
	//    data = fmt.Sprintf("%v\n %v : %v", data, k, v)
	//}
	//data = fmt.Sprintf("%v\n%v", data, string(reqDump))
	data := string(reqDump)
	fmt.Fprintf(writer, "%v", data)
}

type Options struct {
	Https bool
	Addr  string
}

func main() {
	opt := Options{}
	flag.BoolVar(&opt.Https, "https", false, "start a https server")
	flag.StringVar(&opt.Addr, "l", ":28080", "listen address")
	flag.Parse()

	http.HandleFunc("/", HelloFunc)

	srv := http.Server{Addr: opt.Addr}
	var err error
	if opt.Https {
		err = srv.ListenAndServeTLS("server.crt", "server.key")
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil {
		log.Fatalln(err)
	}
}
