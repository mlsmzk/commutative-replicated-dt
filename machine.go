package main

import (
	"log"
	"net/rpc"
)

type Machine int

type Node struct {
}

type View struct {
}

type Model struct {
	Nodes map[string]Node
	Curr  int
	Pos   int
}

var id int
var crdtModel Model



func main() {
	api := new(Machine)
	err := rpc.Register(api)
	if err != nil {
		log.Fatal("error registering the RPCs", err)
	}
}
