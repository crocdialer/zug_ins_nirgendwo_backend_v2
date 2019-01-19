package command

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

// Command realizes a simple RPC interface
type Command struct {
	CommandID int           `json:"id"`
	Command   string        `json:"cmd"`
	Arguments []interface{} `json:"arg"`
}

func (cmd *Command) String() string {
	str := cmd.Command

	for _, arg := range cmd.Arguments {
		str += " " + fmt.Sprintf("%v", arg)
	}
	return str
}

// ACK is used as simple ACK for received commands
type ACK struct {
	Command *Command `json:"command"`
	Success bool     `json:"success"`
	Value   string   `json:"value"`
}

// QueueWorker pulls commands from a channel, sends them via tcp
// and pushes results back to the results channel
type QueueWorker struct {
	TCPAddress string
	Commands   chan *Command
	Results    chan *ACK
}

// NewQueueWorker creates a new instance
func NewQueueWorker(tcpAddress string) *QueueWorker {
	worker := &QueueWorker{
		TCPAddress: tcpAddress,
		Commands:   make(chan *Command, 100),
		Results:    make(chan *ACK, 100),
	}
	go worker.run()
	return worker
}

func (worker *QueueWorker) run() {

	responseBuffer := make([]byte, 1<<11)

	for cmd := range worker.Commands {
		// send the command
		ack := Send(cmd, worker.TCPAddress, responseBuffer)

		// push ACK to result channel
		worker.Results <- ack
	}
}

// Send will send the provided command via tcp to the provided ip-address.
// return: an ACK for the Command
func Send(cmd *Command, ip string, responseBuffer []byte) *ACK {

	if responseBuffer == nil {
		responseBuffer = make([]byte, 1<<11)
	}
	ack := &ACK{Command: cmd}

	// tcp communication with kinskiPlayer here
	con, err := net.Dial("tcp", ip)

	if err == nil {
		defer con.Close()
		str := cmd.String() + "\n"

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
					// log.Println(cmd)
				} else {
					// we got a response here
					response := string(responseBuffer[:bytesRead])
					// log.Println(cmd, "->", response)
					ack.Value = response
				}
			}
		}
	}
	return ack
}

// Playback sends the provided index and playlist to an attached media_player
func Playback(ip string, index int, playlist []string) {

	type Property struct {
		Name  string      `json:"name"`
		Type  string      `json:"type"`
		Value interface{} `json:"value"`
	}

	type ComponentStruct struct {
		Name       string     `json:"name"`
		Properties []Property `json:"properties"`
	}

	comp := ComponentStruct{Name: "media_player"}

	if playlist != nil {
		comp.Properties = append(comp.Properties, Property{
			Name:  "playlist",
			Type:  "string_array",
			Value: playlist,
		})
	}

	comp.Properties = append(comp.Properties, Property{
		Name:  "playlist index",
		Type:  "int",
		Value: index,
	})

	compList := []ComponentStruct{comp}

	// serialize to json
	if jsonBytes, jsonErr := json.Marshal(compList); jsonErr == nil {

		// log.Println(string(jsonBytes))

		// tcp communication with kinskiPlayer here
		con, err := net.Dial("tcp", ip)

		if err == nil {
			defer con.Close()

			// send to player
			if _, writeError := con.Write(jsonBytes); writeError != nil {
				log.Println("could not send json data (Playback)")
			}
		}
	} else {
		log.Println("could not marshal Playback struct to json")
	}
}
