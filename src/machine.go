package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"net/rpc"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type OperationMapID struct {
	opNum int
	opIdx int
}

var pun2op map[int]OperationMapID

var insertMap map[int]*Node
var deleteMap map[int]*Del
var undoMap map[int]*Undo

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
	undo     *Undo
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
	llen    int
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
	undo     *Undo // undo of insertion, nil if insertion is not undone
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
	Nodes map[string]*Node
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
	// fmt.Println("Receiving updates")
	for _, operation := range arguments.NewOperations {
		// lock in case of overlap enqueue, order does not matter (commutative)
		// fmt.Println("Added to remote queue")
		m.Lock()
		remoteQueue.Enqueue(operation)
		m.Unlock()
	}
	reply.Success = true
	return nil
}

func ParseForInsert(rawStr string) *Node {
	var newNode Node
	var depl Depl
	var depr Depr
	fmt.Println(rawStr)
	_, err := fmt.Sscanf(rawStr, "%d %d %d %d %d %d %d %d %d %s",
		&newNode.pid, &newNode.pun, &depl.lpid, &depl.lpun, &depl.loffset,
		&depl.llen, &depr.rpid, &depr.rpun, &depr.roffset, &newNode.str)
	if err != nil {
		panic(err)
	}
	if depl.lpid != -1 {
		newNode.depl = &depl
	}
	if depr.rpid != -1 {
		newNode.depr = &depr
	}
	return &newNode
}

// Send Updates to other Peers regarding new local operations
func BroadcastUpdates() {
	// send outQueue operations to other Peers
	// CHECKOUT: will this handle every single change? even synchronously
	newOps := []string{} // EDIT: do we send a queue or an array? right now its an array
	// fmt.Println("Attempting to broadcast updates")
	for !outQueue.IsEmpty() {
		fmt.Println("OUTQUEUE NOT EMPTY, OPERATING")
		m.Lock()
		outQueueOp := outQueue.Dequeue()
		m.Unlock()
		newOps = append(newOps, outQueueOp)
	}
	for _, peer := range serverPeers {
		// reach out to every peer in the network and send them the operations in this peer
		// fmt.Println("Sending op out to peers")
		go func(peer ServerConnection) {
			args := ReceiveUpdatesArgument{
				NewOperations: newOps,
			}
			var reply ReceiveUpdatesReply
			err := peer.rpcConnection.Call("Machine.ReceiveUpdates", args, &reply)
			if err != nil {
				// fmt.Println("Error is: ", err)
				return
			}
			// fmt.Println("Success")
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
	currNode := crdtModel.Curr
	currPos := crdtModel.Pos

	if currNode == nil {
		return
	}

	//trivial case
	if offset >= 0 && (len(currNode.str)-currPos >= offset) {
		crdtModel.Pos += offset
		return
	}
	if offset < 0 && (currPos >= -offset) {
		crdtModel.Pos += offset
		return
	}

	if offset > 0 {
		offset -= len(currNode.str) - currPos
		for node := currNode.r; node != nil; node = node.r {
			if isNodeVisible(node) {
				if len(node.str) >= offset {
					// could be error here
					crdtModel.Pos = offset
					crdtModel.Curr = node
					return
				} else {
					offset -= len(node.str)
				}
			}
		}
	} else {
		offset += currPos
		for node := currNode.l; node != nil; node = node.l {
			if isNodeVisible(node) {
				if len(node.str) >= -offset {
					// could be error here
					crdtModel.Pos = len(node.str) + offset
					crdtModel.Curr = node
					return
				} else {
					offset += len(node.str)
				}
			}
		}
	}

	/*
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
	*/
}

func RecurseThroughNodes(curr *Node, pos int, offset int, numInvs int) RecurseThroughNodesReply {
	fmt.Println("current str is:", curr.str)
	fmt.Println("node is", curr)
	currNodeLen := len(curr.str)
	fmt.Println("length of node", currNodeLen)
	fmt.Println("pos and offset are:", pos, offset)
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
				reply = RecurseThroughNodes(curr.r, 0, offset, numInvs+1)
			}
		}
	} else {
		if offset > 0 {
			// move to the right
			if offset <= (currNodeLen - pos) {
				pos = pos + offset
				fmt.Println("within the string")
				reply = RecurseThroughNodes(curr, pos, 0, numInvs)
			} else {
				offset = offset - (currNodeLen - pos)
				if curr.r == nil {
					// if there are no more right nodes
					reply = RecurseThroughNodes(curr, currNodeLen, 0, numInvs)
				} else {
					fmt.Println("there is a right node")
					reply = RecurseThroughNodes(curr.r, 0, offset, numInvs)
				}
			}
		} else {
			// if offset < 0 - move to the left
			if pos+offset >= 0 {
				fmt.Println("within the string")
				pos = pos + offset
				reply = RecurseThroughNodes(curr, pos, 0, numInvs)
			} else {
				offset = offset + pos
				if curr.l == nil {
					// if there are no more left nodes
					reply = RecurseThroughNodes(curr, 0, 0, numInvs)
				} else {
					// if there are more left nodes
					fmt.Println("there is a left node")
					reply = RecurseThroughNodes(curr.l, len(curr.l.str), offset, numInvs)
				}
			}
		}
	}
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

func RemoteInsert(newNode *Node) {
	// Case 0: nothing has been inserted yet
	if len(crdtModel.Nodes) == 0 { // First node to be inserted
		fmt.Println("0 insert")
		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		fmt.Println("Key is ", key)
		crdtModel.Nodes[key] = newNode
		fmt.Println("NewNode: ", newNode)
		crdtModel.Curr = newNode
	} else {
		var leftNode *Node
		var rightNode *Node
		leftDep := Depl{
			lpid: -1,
		}
		if newNode.depl != nil {
			leftDep = *newNode.depl
		}
		rightDep := Depr{
			rpid: -1,
		}
		if newNode.depr != nil {
			rightDep = *newNode.depr
		}
		fmt.Println("leftdep: ", leftDep)
		fmt.Println("rightdep: ", rightDep)
		for {
			leftPos := -1
			if leftDep.lpid != -1 {
				leftPos = leftDep.loffset + leftDep.llen
			}
			rightPos := -1
			if rightDep.rpid != -1 {
				rightPos = rightDep.roffset
			}
			var sameDepNodes []*Node
			var tmpNode *Node
			for _, n := range crdtModel.Nodes {
				if (n.depl == nil && leftPos == -1) || (n.depl != nil &&
					n.depl.loffset+n.depl.llen == leftPos && n.depl.lpid == leftDep.lpid && n.depl.lpun == leftDep.lpun) {
					if (n.depr == nil && rightPos == -1) || (n.depr != nil &&
						n.depr.roffset == rightPos && n.depr.rpid == rightDep.rpid && n.depr.rpun == rightDep.rpun) {
						sameDepNodes = append(sameDepNodes, n)
					}
				}
				if newNode.depl != nil && n.pid == newNode.depl.lpid && n.pun == newNode.depl.lpun && n.offset+len(n.str) == leftPos {
					leftNode = n
				}
				if newNode.depr != nil && n.pid == newNode.depr.rpid && n.pun == newNode.depr.rpun && n.offset == rightPos {
					rightNode = n
				}
				if newNode.depl != nil && n.pid == newNode.depl.lpid && n.pun == newNode.depl.lpun && n.offset == newNode.depl.loffset {
					tmpNode = n
				}
			}
			if len(sameDepNodes) == 0 {
				if leftNode == nil && rightNode == nil {
					tmpCurr := crdtModel.Curr
					tmpPos := crdtModel.Pos

					crdtModel.Curr = tmpNode
					crdtModel.Pos = newNode.depl.llen
					curr := crdtModel.Curr

					splitStr := curr.str
					leftStr := splitStr[0:crdtModel.Pos]
					leftNode := curr
					leftNode.str = leftStr
					rightStr := splitStr[crdtModel.Pos:]
					rightNode := *curr // Note : this is dereferenced object
					rightNode.str = rightStr
					rightNode.offset = leftNode.offset + len(leftStr)
					leftNode.r = newNode    // newNode is now to the immediate right of leftNode
					rightNode.l = newNode   // newNode is now to the immediate left of rightNode
					rightNode.il = leftNode // rightNode and leftNode were inserted at the same time, so they have the same ir il
					leftNode.ir = &rightNode

					// Dels replicated to new node
					for i := 0; i < len(leftNode.dels); i++ {
						newDel := *leftNode.dels[i]
						leftNode.dels[i].r = &rightNode
						newDel.l = leftNode
						rightNode.dels[i] = &newDel
					}

					newNode.l = leftNode
					newNode.r = &rightNode
					newNode.depl = &Depl{
						lpid:    newNode.l.pid,
						lpun:    newNode.l.pun,
						loffset: newNode.l.offset,
						llen:    len(newNode.l.str),
					}
					newNode.depr = &Depr{
						rpid:    newNode.r.pid,
						rpun:    newNode.r.pun,
						roffset: newNode.r.offset,
					}
					fmt.Println("left, right", newNode.l, newNode.r)
					key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
					fmt.Println("Key is ", key)
					crdtModel.Nodes[key] = newNode
					fmt.Println("NewNode: ", newNode)
					//rightNode added to Model
					key = strconv.Itoa(rightNode.pid) + "," + strconv.Itoa(rightNode.pun) + "," + strconv.Itoa(rightNode.offset)
					crdtModel.Nodes[key] = &rightNode

					crdtModel.Curr = tmpCurr
					crdtModel.Pos = tmpPos
					break
				}
				newNode.l = leftNode
				newNode.r = rightNode
				if leftNode != nil {
					leftNode.r = newNode
				}
				if rightNode != nil {
					rightNode.l = newNode
				}
				key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
				crdtModel.Nodes[key] = newNode
				break
			} else {
				sameDepNodes = append(sameDepNodes, newNode)
				sort.SliceStable(sameDepNodes, func(i, j int) bool {
					return sameDepNodes[i].pid < sameDepNodes[j].pid ||
						(sameDepNodes[i].pid == sameDepNodes[j].pid && sameDepNodes[i].pun < sameDepNodes[j].pun)
				})
				for i := 0; i < len(sameDepNodes); i++ {
					if sameDepNodes[i] == newNode {
						if i != 0 {
							leftDep.lpid = sameDepNodes[i-1].pid
							leftDep.lpun = sameDepNodes[i-1].pun
							leftDep.loffset = sameDepNodes[i-1].offset
							leftDep.llen = len(sameDepNodes[i-1].str)
						}
						if i != len(sameDepNodes)-1 {
							rightDep.rpid = sameDepNodes[i+1].pid
							rightDep.rpun = sameDepNodes[i+1].pun
							rightDep.roffset = sameDepNodes[i+1].offset
						}
					}
				}
			}
		}
	}
	// find the nodes having same dependency

}

func InsertStr(str string) *Node {
	/* 4 cases: first insert ever, left insert, right insert, or split insert (middle) */
	var newNode = Node{
		pid:      myPid,
		pun:      myPun,
		offset:   0,
		str:      str,
		dels:     nil,
		undo:     nil,
		rendered: false,
		l:        nil,
		r:        nil,
		il:       nil,
		ir:       nil,
		depl:     nil,
		depr:     nil,
	}

	curr := crdtModel.Curr
	// Case 0: nothing has been inserted yet
	if len(crdtModel.Nodes) == 0 { // First node to be inserted
		fmt.Println("0 insert")
		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		fmt.Println("Key is ", key)
		crdtModel.Nodes[key] = &newNode
		fmt.Println("NewNode: ", newNode)
		crdtModel.Curr = &newNode
		crdtModel.Pos = len(str) // For functionality with move; move back to move forward the right number of spaces
	} else if crdtModel.Pos == 0 {
		// Case 1: Insert left
		fmt.Println("left insert")
		newNode.r = curr
		newNode.l = curr.l
		if newNode.r != nil {
			newNode.depr = &Depr{
				rpid:    newNode.r.pid,
				rpun:    newNode.r.pun,
				roffset: newNode.r.offset,
			}
		}
		if newNode.l != nil {
			curr.l.r = &newNode
			newNode.depl = &Depl{
				lpid:    newNode.l.pid,
				lpun:    newNode.l.pun,
				loffset: newNode.l.offset,
				llen:    len(newNode.l.str),
			}
		}
		curr.l = &newNode
		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		crdtModel.Nodes[key] = &newNode
		crdtModel.Curr = &newNode
		crdtModel.Pos = len(str)
	} else if crdtModel.Pos == len(crdtModel.Curr.str) {
		// Case 2: Insert right
		fmt.Println("right insert")
		newNode.l = curr   // set left node to curr,
		newNode.r = curr.r // set right node to right
		if newNode.l != nil {
			newNode.depl = &Depl{
				lpid:    newNode.l.pid,
				lpun:    newNode.l.pun,
				loffset: newNode.l.offset,
				llen:    len(newNode.l.str),
			}
		}
		if newNode.r != nil {
			curr.r.l = &newNode
			newNode.depr = &Depr{
				rpid:    newNode.r.pid,
				rpun:    newNode.r.pun,
				roffset: newNode.r.offset,
			}
		}
		curr.r = &newNode // insert on right (FOR NOW!!!!!!!!!!!!)
		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		crdtModel.Nodes[key] = &newNode
		crdtModel.Curr = &newNode
		crdtModel.Pos = len(str) // For functionality with move; move back to move forward the right number of spaces
	} else {
		// Case 3: The new node will split the current node
		fmt.Println("middle insert")
		splitStr := curr.str
		leftStr := splitStr[0:crdtModel.Pos]
		leftNode := curr
		leftNode.str = leftStr
		rightStr := splitStr[crdtModel.Pos:]
		rightNode := *curr // Note : this is dereferenced object
		rightNode.str = rightStr
		rightNode.offset = leftNode.offset + len(leftStr)
		leftNode.r = &newNode   // newNode is now to the immediate right of leftNode
		rightNode.l = &newNode  // newNode is now to the immediate left of rightNode
		rightNode.il = leftNode // rightNode and leftNode were inserted at the same time, so they have the same ir il
		leftNode.ir = &rightNode

		// Dels replicated to new node
		for i := 0; i < len(leftNode.dels); i++ {
			newDel := *leftNode.dels[i]
			leftNode.dels[i].r = &rightNode
			newDel.l = leftNode
			rightNode.dels[i] = &newDel
		}

		newNode.l = leftNode
		newNode.r = &rightNode
		newNode.depl = &Depl{
			lpid:    newNode.l.pid,
			lpun:    newNode.l.pun,
			loffset: newNode.l.offset,
			llen:    len(newNode.l.str),
		}
		newNode.depr = &Depr{
			rpid:    newNode.r.pid,
			rpun:    newNode.r.pun,
			roffset: newNode.r.offset,
		}
		fmt.Println("left, right", newNode.l, newNode.r)
		key := strconv.Itoa(newNode.pid) + "," + strconv.Itoa(newNode.pun) + "," + strconv.Itoa(newNode.offset)
		fmt.Println("Key is ", key)
		crdtModel.Nodes[key] = &newNode
		fmt.Println("NewNode: ", newNode)
		//rightNode added to Model
		key = strconv.Itoa(rightNode.pid) + "," + strconv.Itoa(rightNode.pun) + "," + strconv.Itoa(rightNode.offset)
		crdtModel.Nodes[key] = &rightNode

		crdtModel.Curr = &newNode
		crdtModel.Pos = len(str) // For functionality with move; move back to move forward the right number of spaces
	}
	return &newNode
}

// odd -> true
func checkOddUndo(undo *Undo) bool {
	numUndo := 0
	for ; undo != nil; undo = undo.undo {
		numUndo++
	}
	return numUndo%2 == 1
}

func isNodeVisible(node *Node) bool {
	if node == nil {
		return false
	}
	if checkOddUndo(node.undo) {
		return false
	}
	for i := 0; i < len(node.dels); i++ {
		if !checkOddUndo(node.dels[i].undo) {
			return false
		}
	}
	return true
}

var leftDel *Node = nil

// Deletes len characters right to the current position.
func DeleteStr(length int) {
	// Assume the cursor is placed only in the node visible
	// cursor should be moved to the right node after deletion
	// assert(len > 0)
	currNode := crdtModel.Curr
	currPos := crdtModel.Pos
	if currPos > 0 {
		var newNode = Node{
			pid:    currNode.pid,
			pun:    currNode.pun,
			offset: currNode.offset + currPos,
			str:    currNode.str[currPos:],
			dels:   nil, //
			undo:   nil,
			l:      currNode,
			r:      currNode.r,
			il:     currNode,
			ir:     currNode.ir,
			depl:   currNode.depl, //currNode
			depr:   currNode.depr, //currNode.depr
		}
		currNode.str = currNode.str[:currPos]
		currNode.r = &newNode
		currNode.ir = &newNode
		currNode.depr = nil //newNode
		currNode = &newNode
		currPos = 0
	}
	if currNode == nil {
		return
	}
	if len(currNode.str) == length {
		var newDel = Del{
			pid:      myPid,
			pun:      myPun,
			l:        leftDel,
			r:        nil,
			undo:     nil,
			rendered: false,
		}
		if leftDel != nil {
			leftDel.r = currNode
		}
		currNode.dels = append(currNode.dels, &newDel)
		leftDel = nil
		if currNode.r == nil {
			for node := currNode.l; node != nil; node = node.l {
				if isNodeVisible(node) {
					crdtModel.Curr = node
					crdtModel.Pos = len(node.str)
				}
			}
		} else {
			crdtModel.Curr = currNode.r
			crdtModel.Pos = 0
		}
	} else if len(currNode.str) < length {
		var newDel = Del{
			pid:      myPid,
			pun:      myPun,
			l:        leftDel,
			r:        nil,
			undo:     nil,
			rendered: false,
		}
		if leftDel != nil {
			leftDel.r = currNode
		}
		currNode.dels = append(currNode.dels, &newDel)
		leftDel = currNode

		node := currNode.r
		// find next visible node
		for ; node != nil; node = node.r {
			if isNodeVisible(node) {
				break
			}
		}
		crdtModel.Curr = node
		crdtModel.Pos = 0
		DeleteStr(length - len(currNode.str))
	} else {
		var newNodeR = Node{
			pid:    currNode.pid,
			pun:    currNode.pun,
			offset: currNode.offset + length,
			str:    currNode.str[length:],
			dels:   nil, //
			undo:   nil,
			l:      currNode,
			r:      currNode.r,
			il:     currNode,
			ir:     currNode.ir,
			depl:   currNode.depl, //currNode
			depr:   currNode.depr, //currNode.depr
		}
		currNode.str = currNode.str[:length]
		currNode.r = &newNodeR
		currNode.ir = &newNodeR
		currNode.depr = nil //newNode
		//currNode = &newNode
		//currPos = 0
		var newDel = Del{
			pid:      myPid,
			pun:      myPun,
			l:        leftDel,
			r:        nil,
			undo:     nil,
			rendered: false,
		}
		currNode.dels = append(currNode.dels, &newDel)
		leftDel = nil
		crdtModel.Curr = currNode.r
		crdtModel.Pos = 0
	}
}

// Undoes op, which can be insert, delete or undo, and the new current position is placed at op.
func UndoOperation(pid int, pun int) {
	/*
		newUndo := Undo{
			pid: myPid,
			pun: myPun,
		}
		opMapID := pun2op(pun)
		switch opMapID.opNum {
		case 0:
			insertMap[opMapID.opIdx]
		case 1:
		case 2:

		}



		var insertMap 	map[int] *Node
		var deleteMap 	map[int] *Del
		var undoMap		map[int] *Undo
		Model.Nodes[]
	*/
	fmt.Println("undo finisheed")
}

func Render() {
	leftMostNode := crdtModel.Curr
	if leftMostNode == nil {
		return
	}
	for ; leftMostNode.l != nil; leftMostNode = leftMostNode.l {
	}
	newViewStr := ""
	newViewPos := 0
	shouldWeAddPos := true
	for node := leftMostNode; node != nil; node = node.r {
		if isNodeVisible(node) {
			newViewStr = newViewStr + node.str
			if shouldWeAddPos {
				if node == crdtModel.Curr {
					shouldWeAddPos = false
					newViewPos += crdtModel.Pos
				} else {
					newViewPos += len(node.str)
				}
			}
		}
	}
	view.str = newViewStr
	view.pos = newViewPos
	fmt.Println("newViewStr: ", newViewStr)
}

// Dequeues operations in the local and remote Queues, and sends broadcast using the outQueue upon new local operations
func DequeueOperations() {
	for {
		if remoteQueue.IsEmpty() && localQueue.IsEmpty() {
			continue
		}
		if !localQueue.IsEmpty() {
			fmt.Println("Dequeuing from local queue")
			for !localQueue.IsEmpty() {
				localQueueOp := localQueue.Dequeue()
				fmt.Println("LocalQueueOp", localQueueOp)
				isMove := false
				switch operation := (checkWhichOp(localQueueOp)); operation {
				case Insert:
					fmt.Println("insert")
					str := localQueueOp[7 : len(localQueueOp)-1]
					// fmt.Println("str", str)
					// this can change to be less exclusive inside insertstr
					newNode := InsertStr(str)
					lpid := -1
					rpid := -1
					var lpun, loffset, llen, rpun, roffset int
					if newNode.depl != nil {
						lpid = newNode.depl.lpid
						lpun = newNode.depl.lpun
						loffset = newNode.depl.loffset
						llen = newNode.depl.llen
					}
					if newNode.depr != nil {
						rpid = newNode.depr.rpid
						rpun = newNode.depr.rpun
						roffset = newNode.depr.roffset
					}
					localQueueOp = fmt.Sprintf("insert(%d %d %d %d %d %d %d %d %d %s)",
						newNode.pid, newNode.pun, lpid, lpun, loffset,
						llen, rpid, rpun, roffset, str)
					fmt.Println("Finished inserting string")
					fmt.Println("View is now: ", view.str)
					// run insert operation
				case Delete:
					fmt.Println("delete")
					str := localQueueOp[7 : len(localQueueOp)-1]
					offset, err := strconv.Atoi(str)
					if err != nil {
						fmt.Println("input to delete is invalid")
						continue
					}
					m.Lock()
					DeleteStr(offset)
					m.Unlock()
					// run delete operation
				case Move:
					fmt.Println("move")
					str := localQueueOp[5 : len(localQueueOp)-1]
					offset, _ := strconv.Atoi(str)
					// m.Lock()
					MoveCursor(offset)
					fmt.Println("moved cursor")
					isMove = true
					// m.Unlock()
				case UndoOp:
					fmt.Println("undo")
					m.Lock()
					UndoOperation(0, 0)
					m.Unlock()
					// run undo operation
				default:
					continue
				}
				m.Lock()
				if !isMove {
					outQueue.Enqueue(localQueueOp)
				}
				myPun += 1
				m.Unlock()
			}
		}
		if !remoteQueue.IsEmpty() {
			fmt.Println("Dequeuing from remote queue")
			for !remoteQueue.IsEmpty() {
				remoteQueueOp := remoteQueue.Dequeue()
				fmt.Println("RemoteQueueOp", remoteQueueOp)
				switch operation := (checkWhichOp(remoteQueueOp)); operation {
				case Insert:
					fmt.Println("remote insert")
					str := remoteQueueOp[7 : len(remoteQueueOp)-1]
					fmt.Println("str", str)
					// this can change to be less exclusive inside insertstr
					newNode := ParseForInsert(str)
					fmt.Println(*newNode)
					RemoteInsert(newNode)
					fmt.Println("Finished inserting string")
					fmt.Println("View is now: ", view.str)
					// run insert operation
				case Delete:
					fmt.Println("delete")
					str := remoteQueueOp[7 : len(remoteQueueOp)-1]
					offset, err := strconv.Atoi(str)
					if err != nil {
						fmt.Println("input to delete is invalid")
						continue
					}
					m.Lock()
					DeleteStr(offset)
					m.Unlock()
					// run delete operation
				case Move:
					fmt.Println("move")
					str := remoteQueueOp[5 : len(remoteQueueOp)-1]
					offset, _ := strconv.Atoi(str)
					// m.Lock()
					MoveCursor(offset)
					fmt.Println("moved cursor")
					// m.Unlock()
				case UndoOp:
					fmt.Println("undo")
					m.Lock()
					UndoOperation(0, 0) // input does not matter
					m.Unlock()
					// run undo operation
				default:
					continue
				}
			}
		}
		// broadcast the new operations done locally
		// wg.Add(1)
		Render()
		go BroadcastUpdates()
		// Go through array and try going through operations
	}
}

func checkWhichOp(op string) int {
	/* Check what kind of operation it is using regex. */
	insertR := "insert\\(.+\\)"
	deleteR := "delete\\([0-9]+\\)"
	undoR := "undo"
	moveR := "move\\(-?[0-9]+\\)"

	match, _ := regexp.MatchString(insertR, op)
	if match {
		return Insert
	}
	match, _ = regexp.MatchString(deleteR, op)
	if match {
		return Delete
	}
	match, _ = regexp.MatchString(undoR, op)
	if match {
		return UndoOp
	}
	match, _ = regexp.MatchString(moveR, op)
	if match {
		return Move
	}
	return -1
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
	crdtModel.Nodes = make(map[string]*Node)
	wg.Add(2)
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

	// run a thread to always be listening to dequeue operations
	go DequeueOperations()

	// listen for terminal input
	go func() {
		for {
			fmt.Println("My Pos is now: ", crdtModel.Pos)
			fmt.Println("My PUN is now: ", myPun)
			fmt.Print("-> ")
			text, _ := reader.ReadString('\n')
			// convert CRLF to LF
			text = strings.Replace(text, "\n", "", -1)
			text = strings.ToLower(text[0 : len(text)-1])
			text = strings.TrimSpace(text)
			// CHECK: maybe can make this cleaner with switch cases?
			// check for matches
			switch operation := (checkWhichOp(text)); operation {
			case Insert:
				fmt.Println("Insert")
				localQueue.Enqueue(text)
			case Delete:
				fmt.Println("Delete")
				localQueue.Enqueue(text)
			case Move:
				fmt.Println("Move")
				localQueue.Enqueue(text)
			case UndoOp:
				fmt.Println("Undo")
				localQueue.Enqueue(text)
			default:
				continue
			}
			fmt.Println(view.str) // print out the up to date string
		}
	}()
	wg.Wait() // Wait forever or until a node crashes
}
