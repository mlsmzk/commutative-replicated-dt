package main

import (
	"log"
	"net/rpc"
)

type Machine int

type OperationID struct {
	pid int
	pun int
}

type Del struct {
	pid      int
	pun      int
	l        *Node
	r        *Node
	undo     Undo
	rendered bool
}

type Undo struct {
	pid      int
	pun      int
	undo     *Undo
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
	pid      int
	pun      int
	offset   int
	str      string
	dels     []Del
	undo     Undo // undo of insertion, nil if insertion is not undone
	rendered bool
	l        *Node
	r        *Node
	il       *Node
	ir       *Node
	depl     Depl
	depr     Depr
}

type View struct {
	str string // character string visible to the user
	pos int    // current position between two characters
}

type Model struct {
	Nodes map[string]Node
	Curr  int
	Pos   int
}

type ReceiveUpdatesArgument struct {
}

type ReceiveUpdatesReply struct {
}

const (
	Insert = iota
	Delete
	UndoOp
)

// ToDo: Import Queue
// var localQueue
// var remoteQueue
// var outQueue

var id int
var crdtModel Model
var view View

// ReceiveUpdates is an RPC used for other machines to call to notify this machine of new remote operations
func (*Machine) ReceiveUpdates(arguments ReceiveUpdatesArgument, reply *ReceiveUpdatesReply) {

}

// Send Updates to other Peers regarding new local operations
func BroadcastUpdates() {
}

// Moves the current position |offset| characters.
// if offset > 0, current position moved to the right. And vice versa to the left
func MoveCursor(offset int) {
}

// Inserts string str at the current position and update current position to be at the right end of str.
func InsertStr(str string) {
}

// Deletes len characters right to the current position.
func DeleteStr(len int) {
}

// Undoes op, which can be insert, delete or undo, and the new current position is placed at op.
func UndoOperation(op int) {
}

func main() {
	api := new(Machine)
	err := rpc.Register(api)
	if err != nil {
		log.Fatal("error registering the RPCs", err)
	}
}
