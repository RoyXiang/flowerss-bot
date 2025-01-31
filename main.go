package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/indes/flowerss-bot/internal/bot"
	_ "github.com/indes/flowerss-bot/internal/log"
	"github.com/indes/flowerss-bot/internal/model"
	"github.com/indes/flowerss-bot/internal/task"
)

func main() {
	model.InitDB()
	task.StartTasks()
	go handleSignal()
	bot.Start()
}

func handleSignal() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	<-c

	bot.Stop()
	task.StopTasks()
	model.Disconnect()
	os.Exit(0)
}
