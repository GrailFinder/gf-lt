package main

import (
	"fmt"
	"net/http"
)

// create server
// listen to the completion endpoint handler

func completion(w http.ResponseWriter, req *http.Request) {
	// post request
	body := req.Body
	// get body as io.reader
	// pass it to the /completion
	go sendMsgToLLM(body)
out:
	for {
		select {
		case chunk := <-chunkChan:
			fmt.Println(chunk)
		case <-streamDone:
			break out
		}
	}
	return
}
