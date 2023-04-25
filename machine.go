package main

import (
	"log"
	"net/rpc"
)

type Machine int

type OperationID struct {
        pid     int
        pun     int
}

type Del struct {
        pid     int
        pun     int
        l       *Node
        r       *Node
        undo
        rendered
}

type Undo struct {
        pid     int
        pun     int
        undo    *Undo
        rendered bool
}

type Depl struct {
        lpid    int
        lpun    int
        loffset int
}
type Depr struct {
        rpid    int
        rpun    int
        roffset int
}

type Node struct {
        pid     int
        pun     int
        offset  int
        str     string
        dels    []Del
        undo    Undo // undo of insertion, nil if insertion is not undone
        rendered bool
        l       *Node
        r       *Node
        il      *Node
        ir      *Node
        depl    Depl
        depr    Depr
}

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
