package main

import (
	"fmt"
	"time"

	"github.com/tandem97/wrapper/debounce"
)

func main() {
	f := debounce.DebounceLast(test, 3*time.Second)

	go func() {
		for i := 0; i <= 5; i++ {
			fmt.Println(f())
			time.Sleep(time.Duration(i) * time.Second)
		}
	}()

	for i := 0; i <= 5; i++ {
		fmt.Println(f())
		time.Sleep(time.Duration(i) * time.Second)
	}
}

func test() (string, error) {
	return "hello", nil
}
