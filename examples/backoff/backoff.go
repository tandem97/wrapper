package main

import (
	"fmt"

	"github.com/tandem97/wrapper/backoff/exponential"
)

func main() {
	backoff := exponential.New()
	for i := 0; i < 10; i++ {
		fmt.Println(backoff.Backoff())
	}

	backoff.Reset()

	fmt.Println(backoff.Backoff())
}
