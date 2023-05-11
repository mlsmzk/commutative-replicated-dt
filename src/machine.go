package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"net/rpc"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	pos int // current position between two characters
}

type Model struct {
	Nodes map[string]Node
	Curr  *Node
	Pos   int
}

type Queue struct {
	item_value []string
}

type ServerConnection struct {
	serverID      int
	Address       string
	rpcConnection *rpc.Client
}

type ReceiveUpdatesArgument struct {
	NewOperations []string // contain the operations in string format in a queue
}

type ReceiveUpdatesReply struct {
	Success bool
}

const (
	Insert = iota
	Delete
	UndoOp
	Move
)

var localQueue Queue
var remoteQueue Queue
var outQueue Queue

var id int
var crdtModel Model
var view View

var wg sync.WaitGroup
var m sync.Mutex

var serverPeers []ServerConnection // does not include itself

// Queue Operations
func (q *Queue) Enqueue(item string) {
	q.item_value = append(q.item_value, item) //used to add items
}

func (q *Queue) Dequeue() string {
	item := q.item_value[0]
	q.item_value = q.item_value[1:] //used to remove items
	return item
}

func (q *Queue) IsEmpty() bool {
	return len(q.item_value) == 0
}

// ReceiveUpdates is an RPC used for other machines to call to notify this machine of new remote operations
func (*Machine) ReceiveUpdates(arguments ReceiveUpdatesArgument, reply *ReceiveUpdatesReply) error {
	// add remote operations received into remoteQueue
	for _, operation := range arguments.NewOperations {
		// lock in case of overlap enqueue, order does not matter (commutative)
		m.Lock()
		remoteQueue.Enqueue(operation)
		m.Unlock()
	}
	reply.Success = true
	return nil
}

// Send Updates to other Peers regarding new local operations
func BroadcastUpdates() {
	// send outQueue operations to other Peers
	// CHECKOUT: will this handle every single change? even synchronously
	newOps := []string{} // EDIT: do we send a queue or an array? right now its an array
	for !outQueue.IsEmpty() {
		m.Lock()
		outQueueOp := outQueue.Dequeue()
		m.Unlock()
		newOps = append(newOps, outQueueOp)
	}
	for _, peer := range serverPeers {
		// reach out to every peer in the network and send them the operations in this peer
		go func(peer ServerConnection) {
			args := ReceiveUpdatesArgument{
				NewOperations: newOps,
			}
			var reply ReceiveUpdatesReply
			peer.rpcConnection.Call("Machine.ReceiveUpdates", args, &reply)
		}(peer)
	}
}

// Moves the current position |offset| characters.
// if offset > 0, current position moved to the right. And vice versa to the left
func MoveCursor(offset int) {
}

// Inserts string str at the current position and update current position to be at the right end of str.
// func InsertStr(str string) {
// 	var newNode = Node{
// 		// pid :
// 		// pun :
// 		// offset :
// 		// str :
// 		// dels :
// 		undo: Undo{
// 			pid: 0, // Will change based on server
// 			pun: 0, // Will change based on number of updates issued
// 			// undo     *Undo
// 			rendered: false,
// 		}, // undo of insertion, nil if insertion is not undone
// 		rendered: false,
// 		// l        *Node
// 		// r        *Node
// 		// il       *Node
// 		// ir       *Node
// 		// depl     Depl
// 		// depr     Dep
// 	}
// 	// Get left and right nodes that are not undone/deleted
// 	curr := Model.Curr
// 	for curr.undo == nil {
// 		curr = curr.l
// 	}
// 	// left = left node
// 	left := curr
// 	curr = Model.Curr
// 	for curr.undo == nil {
// 		curr = curr.right
// 	}
// 	// right = right node
// 	right := curr

// 	// Mark the nodes we split by inserting this one
// 	Model.Curr.il = Model.Curr.ir
// 	Model.Curr.ir = Model.Curr.il
// 	// Not sure if this is right OR IF IT NEEDS TO GO HERE ? !? ?!?

// }

// Deletes len characters right to the current position.
func DeleteStr(len int) {
}

// Undoes op, which can be insert, delete or undo, and the new current position is placed at op.
func UndoOperation(op int) {
	// 
}

// Dequeues operations in the local and remote Queues, and sends broadcast using the outQueue upon new local operations
func DequeueOperations() {
	for {
		if remoteQueue.IsEmpty() && localQueue.IsEmpty() {
			continue
		}
		newOps := []string{}
		if !remoteQueue.IsEmpty() {
			// dequeue the operations once they've been added
			for !remoteQueue.IsEmpty() {
				m.Lock()
				newOps = append(newOps, remoteQueue.Dequeue())
				m.Unlock()
			}
		}
		if !localQueue.IsEmpty() {
			m.Lock()
			localQueueOp := localQueue.Dequeue()
			outQueue.Enqueue(localQueueOp)
			m.Unlock()
			newOps = append(newOps, localQueueOp)
		}
		// broadcast the new operations done locally
		wg.Add(1)
		go BroadcastUpdates()
		// Go through array and try going through operations
		// TO-DO: depends on the format of the strings in the queues
	}
}

func main() {
	// The assumption here is that the command line arguments will contain:
	// This server's ID (zero-based), location and name of the cluster configuration file
	arguments := os.Args
	if len(arguments) == 1 {
		fmt.Println("Please provide cluster information.")
		return
	}

	// Read the values sent in the command line

	// Get this sever's ID (same as its index for simplicity)
	myID, err := strconv.Atoi(arguments[1])
	if err != nil {
		fmt.Println("Failed to get server IDs")
		log.Fatal(err)
	}
	// Get the information of the cluster configuration file containing information on other servers
	file, err := os.Open(arguments[2])
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	myPort := "localhost"

	// Read the IP:port info from the cluster configuration file
	scanner := bufio.NewScanner(file)
	lines := make([]string, 0)
	index := 0
	for scanner.Scan() {
		// Get server IP:port
		text := scanner.Text()
		log.Printf(text, index)
		if index == myID {
			myPort = text
			index++
			continue
		}
		// Save that information as a string for now
		lines = append(lines, text)
		index++
	}
	// If anything wrong happens with readin the file, simply exit
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	api := new(Machine)
	err = rpc.Register(api)
	if err != nil {
		log.Fatal("error registering the RPCs", err)
	}
	rpc.HandleHTTP()
	go http.ListenAndServe(myPort, nil)
	log.Printf("serving rpc on port" + myPort)

	for index, element := range lines {
		// Attemp to connect to the other server node
		client, err := rpc.DialHTTP("tcp", element)
		// If connection is not established
		for err != nil {
			// Record it in log
			log.Println("Trying again. Connection error: ", err)
			// Try again!
			client, err = rpc.DialHTTP("tcp", element)
		}
		// Once connection is finally established
		// Save that connection information in the servers list
		serverPeers = append(serverPeers, ServerConnection{index, element, client})
		// Record that in log
		fmt.Println("Connected to " + element)
	}

	wg.Add(1)

	// read console input
	reader := bufio.NewReader(os.Stdin)
	// Introduction to how to use the terminal
	fmt.Println("Welcome to the Group Editing Terminal:")
	fmt.Println("------------------------------------------")
	fmt.Println("To move your cusor, type the following command where {n} is how many characters you want")
	fmt.Println("to move your cusor by (e.g. n=2 moves the cursor to the right by 2, n=-2 moves the cursor")
	fmt.Println("to the left by 2): move({n})")
	fmt.Println("")
	fmt.Println("To write a new text, type the following command for your insertion where {a} is the word")
	fmt.Println("you want to insert at the current position: insert({n}, {a})")
	fmt.Println("")
	fmt.Println("To delete something, type the following command where {k} is the number of characters")
	fmt.Println("you want to delete to the right of the current position: delete({k})")
	fmt.Println("")
	fmt.Println("To undo your previous command, type the following: undo")

	// regex declaration
	insertR := "insert\\(.+\\)"
	deleteR := "delete\\([0-9]+,[0-9]+\\)"
	undoR := "undo"

	// run a thread to always be listening to dequeue operations
	go DequeueOperations()

	// listen for terminal input
	for {
		fmt.Print("-> ")
		text, _ := reader.ReadString('\n')
		// convert CRLF to LF
		text = strings.Replace(text, "\n", "", -1)
		text = strings.ToLower(text[0 : len(text)-1])
		text = strings.ReplaceAll(text, " ", "") // get rid of whitespaces

		// CHECK: maybe can make this cleaner with switch cases?
		// check for matches
		match, _ := regexp.MatchString(insertR, text)
		if match {
			fmt.Println("insert")
			// run insert operation
		}
		match, _ = regexp.MatchString(deleteR, text)
		if match {
			fmt.Println("delete")
			// run delete operation
		}
		match, _ = regexp.MatchString(undoR, text)
		if match {
			fmt.Println("undo")
			// run undo operation
		}
		fmt.Println(view.str) // print out the up to date string
	}

	wg.Wait() // Wait forever or until a node crashes
}
