package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/crocdialer/zug_ins_nirgendwo_backend_v2/command"
	"github.com/crocdialer/zug_ins_nirgendwo_backend_v2/sse"
	"github.com/gorilla/mux"
)

// http listen port
var listenPort = 8080

// player TCP-port
var playerAddress = "127.0.0.1:33333"

// static serve directory
var serveFilesPath = "./public"

// IO channels
var serialInput, serialOutput chan []byte

// handle for SSE-Server
var sseServer *sse.Server

// next command id
var nextCommandID int32 = 1

// command queue
var commandQueue chan *command.Command

// commands done
var commandsDone chan *command.ACK

// POST
func handleCommand(w http.ResponseWriter, r *http.Request) {

	// configure proper CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// decode json-request
	command := &command.Command{}
	decoder := json.NewDecoder(r.Body)
	decoder.Decode(command)

	// insert struct-type and CommandID
	command.CommandID = int(atomic.AddInt32(&nextCommandID, 1))
	log.Println("command:", command)
	commandQueue <- command

	// ack := CommandACK{CommandID: command.CommandID}

	// encode json ACK and send as response
	enc := json.NewEncoder(w)
	enc.Encode(true)
}

// preflight OPTIONS
func corsHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		} else {
			handler(w, r)
		}
	}
}

func commandQueueWorker(commands <-chan *command.Command, results chan<- *command.ACK) {

	responseBuffer := make([]byte, 1<<12)

	for cmd := range commands {

		// tcp communication with kinskiPlayer here
		con, err := net.Dial("tcp", playerAddress)
		defer con.Close()

		if err == nil {
			str := cmd.String() + "\n"
			ack := &command.ACK{Command: cmd}

			// send to player
			if _, writeError := con.Write([]byte(str)); writeError == nil {

				// command could be transferred
				ack.Success = true

				// timeout 50ms
				timeOut := time.Now().Add(time.Millisecond * 50)

				if deadLineErr := con.SetReadDeadline(timeOut); deadLineErr != nil {
					log.Fatal(deadLineErr)
				} else {

					if bytesRead, readError := con.Read(responseBuffer); readError != nil {
						// log.Println(readError)
						log.Println(cmd)
					} else {
						// we got a response here
						response := string(responseBuffer[:bytesRead])
						log.Println(cmd, "->", response)
						ack.Value = response
					}
				}

				// push ACK to result channel
				results <- ack
			}
		}
	}
}

func commandQueueCollector(results <-chan *command.ACK) {
	for ack := range results {
		// log.Println("sse:", cmd)

		// emit SSE update
		sseServer.ACKQueue <- ack
	}
}

func main() {
	log.Println("welcome", os.Args[0])

	// get serve path
	if len(os.Args) > 1 {
		serveFilesPath = os.Args[1]
	}

	// get server port
	if len(os.Args) > 2 {
		if p, err := strconv.Atoi(os.Args[2]); err == nil {
			listenPort = p
		}
	}

	commandQueue = make(chan *command.Command, 100)
	commandsDone = make(chan *command.ACK, 100)

	// start command processing
	go commandQueueWorker(commandQueue, commandsDone)
	go commandQueueCollector(commandsDone)

	// serve static files
	fs := http.FileServer(http.Dir(serveFilesPath))

	// serves eventstream
	sseServer = sse.NewServer()

	// create a gorilla mux-router
	muxRouter := mux.NewRouter()

	// http services
	muxRouter.Handle("/events", sseServer)
	muxRouter.HandleFunc("/cmd", corsHandler(handleCommand)).Methods("POST", "OPTIONS")
	muxRouter.PathPrefix("/").Handler(fs)
	http.Handle("/", muxRouter)

	log.Println("server listening on port", listenPort, " -- serving files from", serveFilesPath)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", listenPort), nil))
}
