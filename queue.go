package main

import "fmt"

type Queue struct {
	item_value []int
}

func (q *Queue) Enqueue(item int) {
	q.item_value = append(q.item_value, item) //used to add items
}

func (q *Queue) Dequeue() int {
	item := q.item_value[0]
	q.item_value = q.item_value[1:] //used to remove items
	return item
}

func (q *Queue) IsEmpty() bool {
	return len(q.item_value) == 0
}

func main() {
	fmt.Println("Enqueue and Dequeue the elements:")
	q := &Queue{} // create a queue instance which will be used to enqueue and dequeue elements
	q.Enqueue(1)
	fmt.Println("Queue is now: ", q)
	q.Enqueue(2)
	fmt.Println("Queue is now: ", q)
	q.Enqueue(3)
	fmt.Println("Queue is now: ", q)
	for !q.IsEmpty() {
		fmt.Println(q.Dequeue()) //check whether the queue is empty or not
		fmt.Println("Queue is now: ", q)
	}
}
