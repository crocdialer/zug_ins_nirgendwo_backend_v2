package command

import (
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
	Command *Command    `json:"command"`
	Success bool        `json:"success"`
	Value   interface{} `json:"value"`
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

	responseBuffer := make([]byte, 1<<12)

	for cmd := range worker.Commands {

		ack := &ACK{Command: cmd}

		// tcp communication with kinskiPlayer here
		con, err := net.Dial("tcp", worker.TCPAddress)

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
						log.Println(cmd)
					} else {
						// we got a response here
						response := string(responseBuffer[:bytesRead])
						log.Println(cmd, "->", response)
						ack.Value = response
					}
				}
			}
		}
		// push ACK to result channel
		worker.Results <- ack
	}
}
