package command

import "fmt"

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
