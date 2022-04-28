package main

import (
	"context"
	"log"
	"net"
	"os"
	"time"
)

func handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		log.Println(err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var buf [128]byte
			n, err := conn.Read(buf[:])
			if err != nil {
				log.Print(err)
				return
			}
			os.Stderr.Write(buf[:n])
		}
	}
}
