package main

import (
	"context"
	"log"
	"time"
)

// runWorker fans events off a channel using a goroutine.
func runWorker(ctx context.Context, events <-chan string) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				processEvent(e)
			}
		}
	}()
}

func processEvent(e string) {
	log.Printf("processed %s at %s", e, time.Now())
}
