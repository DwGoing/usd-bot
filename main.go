package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/DwGoing/usd-bot/internal/app"
	"github.com/alibaba/ioc-golang"
	"github.com/alibaba/ioc-golang/config"
)

func main() {
	if err := ioc.Load(config.WithSearchPath(".")); err != nil {
		panic(err)
	}
	app, err := app.GetAppSingleton()
	if err != nil {
		panic(err)
	}
	err = app.Initialize()
	if err != nil {
		panic(err)
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
}
