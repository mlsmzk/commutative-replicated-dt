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

type Deps struct {
	OpPid int // pid of remote insertor
	OpPun int
	OpOff int
	Left  *Depl
	Right *Depr
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
	dels     []*Del
	undo     bool // undo of insertion, nil if insertion is not undone
	rendered bool
	l        *Node
	r        *Node
	il       *Node
	ir       *Node
	depl     *Depl
	depr     *Depr
}

type View struct {
	str string // character string visible to the user
	pos int    // current position between two characters
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

var myPid int
var myPun int

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
		fmt.Println("OUTQUEUE NOT EMPTY, OPERATING")
		m.Lock()
		outQueueOp := outQueue.Dequeue()
		m.Unlock()
		newOps = append(newOps, outQueueOp)
	}
	for _, peer := range serverPeers {
		// reach out to every peer in the network and send them the operations in this peer
		fmt.Println("Sending op out to peers")
		go func(peer ServerConnection) {
			args := ReceiveUpdatesArgument{
				NewOperations: newOps,
			}
			var reply ReceiveUpdatesReply
			peer.rpcConnection.Call("Machine.ReceiveUpdates", args, &reply)
		}(peer)
	}
}

type RecurseThroughNodesReply struct {
	node    *Node
	posNode int
}

// Moves the current position |offset| characters.
// if offset > 0, current position moved to the right. And vice versa to the left
func MoveCursor(offset int) {
	// Update the Curr Node and Pos of the model
	// Update the position of the View
	fmt.Println("offset is", offset)
	if offset == 0 {
		return
	}
	curr := crdtModel.Curr
	currPos := crdtModel.Pos
	if offset > 0 {
		if view.pos+offset > len(view.str) {
			// check if the curr will move past the end of the string in view=
			view.pos = len(view.str)
		} else {
			view.pos = view.pos + offset
		}

	} else {
		if view.pos+offset < len(view.str) {
			// check if the curr will move past the beginning of the string in view
			view.pos = 0
		} else {
			view.pos = view.pos + offset
		}
	}
	fmt.Println("new post in view:", view.pos)
	reply := RecurseThroughNodes(curr, currPos, offset, 0)
	crdtModel.Curr = reply.node
	crdtModel.Pos = reply.posNode
	fmt.Println("hi im here :", reply.node)
	fmt.Println("at this position:", reply.posNode)
}

func RecurseThroughNodes(curr *Node, pos int, offset int, numInvs int) RecurseThroughNodesReply {
	fmt.Println("current str is:", curr.str)
	fmt.Println("node is", curr)
	currNodeLen := len(curr.str)
	fmt.Println("length of node", currNodeLen)
	reply := RecurseThroughNodesReply{
		node:    curr,
		posNode: pos,
	}
	if offset == 0 {
		fmt.Println("final pos:", pos)
		fmt.Println("final str:", curr.str)
		// if no more need to recurse through nodes
		return reply
	}
	if !curr.rendered {
		// if the node isn't being rendered
		if offset < 0 {
			// wants to move to the left
			if curr.l == nil {
				// if there are no more left nodes, go to the previous non-invisible node
				var finalNode *Node
				for numInvs > 0 {
					finalNode = curr.r
					numInvs = numInvs - 1
				}
				reply = RecurseThroughNodes(finalNode, 0, 0, 0)
			} else {
				reply = RecurseThroughNodes(curr.l, len(curr.l.str), offset, numInvs+1)
			}
		} else {
			// wants to move to the right
			if curr.r == nil {
				fmt.Println("right node is empty")
				// if there are no more right nodes, go to the previous non-invisible node
				var finalNode *Node
				for numInvs > 0 {
					finalNode = curr.l
					numInvs = numInvs - 1
				}
				reply = RecurseThroughNodes(finalNode, len(finalNode.str), 0, 0)
			} else {
				reply = RecurseThroughNodes(curr.r, len(curr.l.str), offset, numInvs+1)
			}
		}
	} else {
		fmt.Println("sum value is", pos+offset)
		if pos+offset >= 0 && pos+offset <= currNodeLen {
			fmt.Println("within the string")
			pos = pos + offset
			fmt.Println("new pos:", pos)
			reply = RecurseThroughNodes(curr, pos, 0, numInvs)
		} else {
			offset = offset + pos
			if offset < 0 {
				if curr.l == nil {
					// if there are no more left nodes
					reply = RecurseThroughNodes(curr, 0, 0, numInvs)
				} else {
					// if there are more left nodes
					reply = RecurseThroughNodes(curr.l, len(curr.l.str), offset, numInvs)
				}
			} else {
				// has to be greater than the length of the current node - pos+offset == 0 is accounted for earlier
				if curr.r == nil {
					// if there are no more right nodes
					reply = RecurseThroughNodes(curr, currNodeLen, 0, numInvs)
				} else {
					fmt.Println("there is a right node")
					reply = RecurseThroughNodes(curr.r, 0, offset, numInvs)
				}
			}
		}
	}
	// else if offset < 0{
	// 	// keep going to the left
	// 	if pos + offset >= 0{
	// 		// can stop at this node
	// 		pos = pos + offset
	// 		RecurseThroughNodes(curr, pos, 0) // can stop recursing
	// 	} else {
	// 		// need to continue to the next node
	// 		offset = offset + pos
	// 		if curr.l == nil {
	// 			// if there are no more left nodes
	// 			RecurseThroughNodes(curr, 0, 0)
	// 		} else {
	// 			// if there are more left nodes
	// 			RecurseThroughNodes(curr.l, len(curr.l.str), offset)
	// 		}
	// 	}
	// } else {
	// 	// keep going to the right
	// 	if pos + offset <= currNodeLen {
	// 		// can stop at this node
	// 		pos = pos + offset
	// 		RecurseThroughNodes(curr, pos, 0) // can stop recursing
	// 	} else {
	// 		// need to continue to the next node
	// 		offset = offset + pos
	// 		if curr.r == nil {
	// 			// if there are no more right nodes
	// 			RecurseThroughNodes(curr, currNodeLen, 0)
	// 		} else {
	// 			RecurseThroughNodes(curr.r, 0, offset)
	// 		}
	// 	}
	// }
	return reply
}

// Inserts string str at the current position and update current position to be at the right end of str.
func ArrayEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func InsertStr(str string, deps ...*Deps) {
	/* 4 cases: first insert ever, left insert, right insert, or split insert (middle) */
	var newNode = Node{
		pid:      myPid,
		pun:      myPun,
		offset:   crdtModel.Pos,
		str:      str,
		dels:     nil,
		undo:     false,
		rendered: true,
		l:        nil,
		r:        nil,
		il:       nil,
		ir:       nil,
		depl:     nil,
		depr:     nil,
	}
	for _, d := range deps { // Get info from remote node if there are dependencies
		if d.OpPid != myPid {
			fmt.Println("PID is not mine, must be remote. Setting new pid, pun, and offset")
			newNode.pid = d.OpPid
			newNode.pun = d.OpPun
			newNode.offset = d.OpOff
			newNode.depl = d.Left
			newNode.depr = d.Right
			fmt.Println("New PPO set, newNode is now: ", newNode)
		}
	}
	// Case 0: nothing has been inserted yet
	if len(crdtModel.Nodes) == 0 { // First node to be inserted
		fmt.Println("0 insert")
		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		fmt.Println("Key is ", key)
		crdtModel.Nodes[key] = newNode
		fmt.Println("NewNode: ", newNode)
		crdtModel.Curr = &newNode
		crdtModel.Pos = 0 // For functionality with move; move back to move forward the right number of spaces
		view.str += str
		outQueue.Enqueue("insert(" + str + ")")
		return
	}
	curr := crdtModel.Curr
	// Case 1: Insert left
	if crdtModel.Pos <= curr.offset {
		fmt.Println("left insert")
		newNode.r = curr
		curr.l = &newNode
		newNode.l = nil
		fmt.Println("left, right", newNode.l, newNode.r)

		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		fmt.Println("Key is ", key)
		crdtModel.Nodes[key] = newNode
		fmt.Println("NewNode: ", newNode)
		crdtModel.Curr = &newNode
		crdtModel.Pos = 0 // For functionality with move; move back to move forward the right number of spaces
		view.str = str + view.str
		outQueue.Enqueue("insert(" + str + ")")
		fmt.Println("Outqueue is: ", outQueue)
	}
	// Case 2: Insert right
	if crdtModel.Pos >= curr.offset+len(curr.str) {
		fmt.Println("right insert")
		curr.r = &newNode          // insert on right (FOR NOW!!!!!!!!!!!!)
		newNode.l = crdtModel.Curr // set left node to curr,
		// which will be to this new node's immediate left
		newNode.r = nil // set right node to right
		fmt.Println("left, right", newNode.l, newNode.r)
		// if !(left == nil && right == nil) && ArrayEqual([]int{left.pid, left.pun, left.offset}, []int{right.pid, right.pun, right.offset}) { // Check if they are the same insert op
		// 	left.ir = right // if yes, link them to each other
		// 	right.il = left // don't need to worry about ir and il of left because a node can't be split up other than in between two parts of it
		// 	// i.e. this doesn't matter if the left node is a different insert op than the right node
		// }

		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		fmt.Println("Key is ", key)
		crdtModel.Nodes[key] = newNode
		fmt.Println("NewNode: ", newNode)
		crdtModel.Curr = &newNode
		crdtModel.Pos = 0 // For functionality with move; move back to move forward the right number of spaces
		view.str += str
		outQueue.Enqueue("insert(" + str + ")")
		fmt.Println("Outqueue is: ", outQueue)
	}
	// Case 3: The new node will split the current node
	if crdtModel.Pos > curr.offset && crdtModel.Pos < curr.offset+len(curr.str) {
		fmt.Println("middle insert")
		leftNode := curr
		rightNode := curr
		leftNode.r = &newNode   // newNode is now to the immediate right of leftNode
		rightNode.l = &newNode  // newNode is now to the immediate left of rightNode
		rightNode.il = leftNode // rightNode and leftNode were inserted at the same time, so they have the same ir il
		leftNode.ir = rightNode

		fmt.Println("left, right", newNode.l, newNode.r)
		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		fmt.Println("Key is ", key)
		crdtModel.Nodes[key] = newNode
		fmt.Println("NewNode: ", newNode)
		crdtModel.Curr = &newNode
		crdtModel.Pos = 0 // For functionality with move; move back to move forward the right number of spaces

		// Update view
		leftBound := crdtModel.Pos
		// rightBound := crdtModel.Pos + len(newNode.str)
		view.str = view.str[0:leftBound] + newNode.str + view.str[leftBound:len(view.str)]
		outQueue.Enqueue("insert(" + str + ")")
		fmt.Println("Outqueue is: ", outQueue)
	}
}

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
	myPid = myID
	myPun = 0
	crdtModel.Nodes = make(map[string]Node)
	wg.Add(1)
	fmt.Println("myPid", myPid)
	fmt.Println("myPun", myPun)
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
	fmt.Println("you want to insert at the current position: insert({a})")
	fmt.Println("")
	fmt.Println("To delete something, type the following command where {k} is the number of characters")
	fmt.Println("you want to delete to the right of the current position: delete({k})")
	fmt.Println("")
	fmt.Println("To undo your previous command, type the following: undo")
	fmt.Println("")

	// regex declaration
	insertR := "insert\\(.+\\)"
	deleteR := "delete\\([0-9]+\\)"
	undoR := "undo"
	moveR := "move\\([0-9]+\\)"

	// run a thread to always be listening to dequeue operations
	go DequeueOperations()

	// listen for terminal input
	for {
		fmt.Print("-> ")
		fmt.Println("My PUN is now: ", myPun)
		text, _ := reader.ReadString('\n')
		// convert CRLF to LF
		text = strings.Replace(text, "\n", "", -1)
		text = strings.ToLower(text[0 : len(text)-1])
		text = strings.TrimSpace(text)
		// CHECK: maybe can make this cleaner with switch cases?
		// check for matches
		match, _ := regexp.MatchString(insertR, text)
		if match {
			fmt.Println("insert")
			str := text[7 : len(text)-1]
			fmt.Println("str", str)
			m.Lock()
			InsertStr(str)
			myPun += 1
			m.Unlock()
			// run insert operation
		}
		match, _ = regexp.MatchString(deleteR, text)
		if match {
			fmt.Println("delete")
			m.Lock()
			// DeleteStr(str)
			myPun += 1
			m.Unlock()
			// run delete operation
		}
		match, _ = regexp.MatchString(undoR, text)
		if match {
			fmt.Println("undo")
			m.Lock()
			// UndoOperation(str)
			myPun += 1
			m.Unlock()
			// run undo operation
		}
		match, _ = regexp.MatchString(moveR, text)
		if match {
			fmt.Println("move")
			str := text[5 : len(text)-1]
			offset, _ := strconv.Atoi(str)
			// m.Lock()
			MoveCursor(offset)
			fmt.Println("moved cursor")
			// m.Unlock()
		}
		fmt.Println(view.str) // print out the up to date string
	}
	wg.Wait() // Wait forever or until a node crashes
}
