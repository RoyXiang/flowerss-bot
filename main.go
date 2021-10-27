package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/indes/flowerss-bot/bot"
	"github.com/indes/flowerss-bot/internal/task"
	_ "github.com/indes/flowerss-bot/log"
	"github.com/indes/flowerss-bot/model"
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

	task.StopTasks()
	model.Disconnect()
	os.Exit(0)
}
