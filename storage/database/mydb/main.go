package main

import "flag"

var (
	client = flag.Bool("client", true, "run in client mode")
	server = flag.Bool("server", false, "run in server mode")
	port   = flag.Uint("port", 19917, "server listen port default 19917")
	host   = flag.String("host", "127.0.0.1", "server listen host")
)

func main() {
	flag.Parse()
	if *server {
		runServer()
		return
	}
	runClient()
}
