package main

import "log"

// Command stores the command line options and arguments
type Command struct {
	URL      string
	FileName string
}

// execute runs this command
func (cmd *Command) execute() {
	if !cmd.validate() {
		log.Println("Invalid command")
		return
	}

	w := &Work{}
	w.parse(*cmd)

	w.run()
}

func (cmd *Command) validate() bool {
	if !validateURL(cmd.URL) {
		log.Fatal(cmd.URL, " is invalid or not supported")
		return false
	}
	return true
}
