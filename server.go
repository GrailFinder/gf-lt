package main

import (
	"elefant/config"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Server struct {
	// nolint
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
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

// create server
// listen to the completion endpoint handler
func pingHandler(w http.ResponseWriter, req *http.Request) {
	if _, err := w.Write([]byte("pong")); err != nil {
		logger.Error("server ping", "error", err)
	}
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
			fmt.Print(chunk)
			if _, err := w.Write([]byte(chunk)); err != nil {
				logger.Warn("failed to write chunk", "value", chunk)
				continue
			}
		case <-streamDone:
			break out
		}
	}
}

func modelHandler(w http.ResponseWriter, req *http.Request) {
	llmModel := fetchModelName()
	payload, err := json.Marshal(llmModel)
	if err != nil {
		logger.Error("model handler", "error", err)
		// return err
		return
	}
	if _, err := w.Write(payload); err != nil {
		logger.Error("model handler", "error", err)
	}
}
