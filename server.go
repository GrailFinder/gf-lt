package main

import (
	"elefant/config"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Server struct {
	config config.Config
}

func (srv *Server) ListenToRequests(port string) {
	// h := srv.actions
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:         "localhost:" + port,
		Handler:      mux,
		ReadTimeout:  time.Second * 5,
		WriteTimeout: time.Second * 5,
	}
	mux.HandleFunc("GET /ping", pingHandler)
	mux.HandleFunc("GET /model", modelHandler)
	mux.HandleFunc("POST /completion", completionHandler)
	fmt.Println("Listening", "addr", server.Addr)
	server.ListenAndServe()
}

// create server
// listen to the completion endpoint handler
func pingHandler(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("pong"))
}

func completionHandler(w http.ResponseWriter, req *http.Request) {
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
			w.Write([]byte(chunk))
		case <-streamDone:
			break out
		}
	}
	return
}

func modelHandler(w http.ResponseWriter, req *http.Request) {
	llmModel := fetchModelName()
	payload, err := json.Marshal(llmModel)
	if err != nil {
		// return err
	}
	w.Write(payload)
}
