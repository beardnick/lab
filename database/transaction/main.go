package main

import (
	"context"
	"fmt"

	"github.com/jessevdk/go-flags"
)

type ServerOptions struct {
	Host string `short:"h" long:"host" default:"0.0.0.0" description:"database listen host"`
	Port int    `short:"p" long:"port" default:"13306" description:"database listen port"`
}

func main() {
	opt := ServerOptions{}
	flags.Parse(&opt)
	server := Server{
		database: &DataBase{},
		address:  fmt.Sprintf("%s:%d", opt.Host, opt.Port),
	}
	server.start(context.Background())
}
