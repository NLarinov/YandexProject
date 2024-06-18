package main

import (
	"sync"
	"time"
)

// Operation structure for storing operation information
type Operation struct {
	UserID      int
	OperationID string // unique number for each operation
	Expression  string // mathematical expression
	Start       string // start time of operation
	Duration    string // duration of operation
	Status      string // status of operation (done, proceeding, free)
	HeartBeat   string // period of sending info from goroutine
	GoroutineId int
	timer       chan interface{} // close channel
}

// User structure for reading from db
type User struct {
	Id       int
	Login    string
	Password string
	Token    string
}

// Cache structure for cache, which includes all current running operations
type Cache struct {
	userData          map[string]User      // key Login, value User
	operData          map[string]Operation // key OperationId, value Operation
	interval          time.Duration        // refresh duration for calculatingServer
	heartBeatDuration time.Duration        // refresh duration for calculate
	stop              chan struct{}        // close channel
	locker            sync.RWMutex
}
