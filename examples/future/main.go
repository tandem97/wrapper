package main

import (
	"log"
	"time"

	"github.com/tandem97/wrapper/future"
)

func Hello() (string, error) {
	log.Println("hello")
	time.Sleep(5 * time.Second)

	return "world", nil
}

func main() {
	wrappedHello := future.WrapSlowFunc(Hello)
	res, _ := wrappedHello.Result()

	log.Println(res)
}
